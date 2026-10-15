package fileops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A trimmed `ffmpeg -encoders` listing: the legend, then audio and video rows.
const encodersListing = `Encoders:
 V..... = Video
 A..... = Audio
 ------
 V....D libx264              libx264 H.264
 A....D aac                  AAC (Advanced Audio Coding)
 A....D flac                 FLAC (Free Lossless Audio Codec)
 A....D libmp3lame           libmp3lame MP3 (MPEG audio layer 3) (codec mp3)
 A....D libopus              libopus Opus (codec opus)
 A....D pcm_s16le            PCM signed 16-bit little-endian
 A..X.D vorbis               Vorbis
`

func TestParseEncodersKeepsAudioOnly(t *testing.T) {
	have := parseEncoders([]byte(encodersListing))
	for _, want := range []string{"aac", "flac", "libmp3lame", "libopus", "pcm_s16le", "vorbis"} {
		if !have[want] {
			t.Errorf("%s missing from %v", want, have)
		}
	}
	for _, not := range []string{"libx264", "=", "Audio"} {
		if have[not] {
			t.Errorf("%s should not be an audio encoder", not)
		}
	}
}

// Without libvorbis, .ogg falls back to ffmpeg's experimental encoder and says
// so; a format with no encoder at all is not offered.
func TestAvailableAudioFormatsResolvesEncoders(t *testing.T) {
	got := availableAudioFormats(parseEncoders([]byte(encodersListing)))
	byFormat := map[AudioFormat]AudioFormatInfo{}
	for _, i := range got {
		byFormat[i.Format] = i
	}

	ogg, ok := byFormat[AudioOgg]
	if !ok || ogg.Encoder != "vorbis" || !ogg.Experimental {
		t.Errorf("ogg = %+v, want the experimental vorbis encoder", ogg)
	}
	if m4a := byFormat[AudioM4A]; m4a.Encoder != "aac" || m4a.Experimental {
		t.Errorf("m4a = %+v, want aac", m4a)
	}
	if len(got) != 6 {
		t.Errorf("got %d formats, want all 6", len(got))
	}

	with := availableAudioFormats(map[string]bool{"libvorbis": true, "vorbis": true})
	if len(with) != 1 || with[0].Encoder != "libvorbis" || with[0].Experimental {
		t.Errorf("with libvorbis = %+v, want libvorbis preferred", with)
	}
	if none := availableAudioFormats(map[string]bool{}); len(none) != 0 {
		t.Errorf("no encoders offered %d formats", len(none))
	}
}

func TestFFmpegArgs(t *testing.T) {
	o := ConvertOpts{Format: AudioOgg, Quality: 1, Encoder: "vorbis"}
	args := strings.Join(ffmpegArgs("/in/a.mp3", "/out/.a.ogg.tyr-1", o), " ")
	for _, want := range []string{
		"-nostdin", "-i /in/a.mp3", "-map 0:a:0", "-c:a vorbis",
		"-strict experimental", "-ac 2", "-q:a 5", "-f ogg /out/.a.ogg.tyr-1",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}

	lib := strings.Join(ffmpegArgs("/in/a.mp3", "/t", ConvertOpts{Format: AudioOgg, Quality: 1, Encoder: "libvorbis"}), " ")
	if strings.Contains(lib, "-strict") || strings.Contains(lib, "-ac") {
		t.Errorf("libvorbis should need no workarounds: %q", lib)
	}
}

// A source whose name starts with "-" must not reach ffmpeg as an option.
func TestFFmpegArgsGuardOptionLikeNames(t *testing.T) {
	args := ffmpegArgs("-y.mp3", "-t", ConvertOpts{Format: AudioWAV, Encoder: "pcm_s16le"})
	i := slices.Index(args, "-i")
	if i < 0 || strings.HasPrefix(args[i+1], "-") {
		t.Errorf("input passed as %q", args[i+1])
	}
	if last := args[len(args)-1]; strings.HasPrefix(last, "-") {
		t.Errorf("output passed as %q", last)
	}
}

func TestConvertTarget(t *testing.T) {
	if got := ConvertTarget("/a/b/Song.Name.mp3", "/c", AudioOpus); got != filepath.Join("/c", "Song.Name.opus") {
		t.Errorf("got %q", got)
	}
}

func TestIsAudio(t *testing.T) {
	for name, want := range map[string]bool{
		"a.mp3": true, "B.FLAC": true, "clip.mkv": true, "notes.txt": false, "mp3": false,
	} {
		if got := IsAudio(name); got != want {
			t.Errorf("IsAudio(%q) = %v, want %v", name, got, want)
		}
	}
}

// runConvert drives a convert job against fake tools, answering any conflict
// with answer.
func runConvert(t *testing.T, f *fakeTools, job Job, answer ConflictAction) Result {
	t.Helper()
	ch := make(chan Event)
	ctx := context.Background()
	r := &reporter{ctx: ctx, ch: ch, tools: f}

	done := make(chan Result, 1)
	go func() {
		created, notes, err := convert(ctx, job, r, newResolver(ctx, ch, job.OnConflict))
		done <- Result{Op: job.Op, Err: err, Created: created, Notes: notes}
		close(ch)
	}()
	for ev := range ch {
		if c, ok := ev.(Conflict); ok {
			c.Reply <- Resolution{Action: answer}
		}
	}
	select {
	case res := <-done:
		return res
	case <-time.After(10 * time.Second):
		t.Fatal("job never finished")
		return Result{}
	}
}

func convertJob(t *testing.T, names ...string) (Job, string) {
	t.Helper()
	src, dst := t.TempDir(), t.TempDir()
	var srcs []string
	for _, n := range names {
		p := filepath.Join(src, n)
		if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}
	return Job{
		Op: OpConvert, Srcs: srcs, Dest: dst,
		Convert: ConvertOpts{Format: AudioOgg, Quality: 1, Encoder: "libvorbis"},
	}, dst
}

// Each source is converted into a temporary name, then renamed into place.
func TestConvertRenamesIntoPlace(t *testing.T) {
	job, dst := convertJob(t, "a.mp3", "b.flac")
	f := &fakeTools{}
	res := runConvert(t, f, job, ConflictAsk)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if n := len(ffmpegRuns(f)); n != 2 {
		t.Fatalf("ran ffmpeg %d times, want 2", n)
	}
	want := []string{filepath.Join(dst, "a.ogg"), filepath.Join(dst, "b.ogg")}
	if !slices.Equal(res.Created, want) {
		t.Errorf("created %v, want %v", res.Created, want)
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 2 {
		t.Errorf("dest holds %d entries, want just the 2 outputs", len(entries))
	}
}

// A failing ffmpeg leaves nothing behind, not even its temporary file, and
// does not disturb a file that was already there.
func TestConvertFailureCleansUp(t *testing.T) {
	job, dst := convertJob(t, "a.mp3")
	old := filepath.Join(dst, "a.ogg")
	if err := os.WriteFile(old, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeTools{err: errors.New("ffmpeg failed: boom")}
	res := runConvert(t, f, job, ConflictOverwrite)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "a.mp3") {
		t.Fatalf("err = %v, want one naming the source", res.Err)
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 1 {
		t.Errorf("dest holds %d entries, want only the original", len(entries))
	}
	if b, _ := os.ReadFile(old); string(b) != "keep me" {
		t.Errorf("existing output was touched: %q", b)
	}
}

func TestConvertConflictSkipAndKeepBoth(t *testing.T) {
	job, dst := convertJob(t, "a.mp3")
	if err := os.WriteFile(filepath.Join(dst, "a.ogg"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeTools{}
	if res := runConvert(t, f, job, ConflictSkip); res.Err != nil || len(res.Created) != 0 || len(f.ran) != 0 {
		t.Errorf("skip: err=%v created=%v ran=%d", res.Err, res.Created, len(f.ran))
	}

	res := runConvert(t, &fakeTools{}, job, ConflictKeepBoth)
	if want := filepath.Join(dst, "a (2).ogg"); res.Err != nil || !slices.Equal(res.Created, []string{want}) {
		t.Errorf("keep both: err=%v created=%v", res.Err, res.Created)
	}
}

// Overwriting a file that was already there is not something undo can take
// back, so it is not reported as created.
func TestConvertOverwriteIsNotCreated(t *testing.T) {
	job, dst := convertJob(t, "a.mp3", "b.mp3")
	if err := os.WriteFile(filepath.Join(dst, "a.ogg"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res := runConvert(t, &fakeTools{}, job, ConflictOverwrite)
	if want := []string{filepath.Join(dst, "b.ogg")}; res.Err != nil || !slices.Equal(res.Created, want) {
		t.Errorf("err=%v created=%v, want %v", res.Err, res.Created, want)
	}
}

func TestConvertRefusesToOverwriteSource(t *testing.T) {
	job, _ := convertJob(t, "a.ogg")
	job.Dest = filepath.Dir(job.Srcs[0])
	f := &fakeTools{}
	if res := runConvert(t, f, job, ConflictOverwrite); res.Err == nil || len(f.ran) != 0 {
		t.Errorf("err=%v ran=%d, want a refusal before ffmpeg runs", res.Err, len(f.ran))
	}
}

func ffmpegRuns(f *fakeTools) []toolCmd {
	var out []toolCmd
	for _, c := range f.ran {
		if c.bin == "ffmpeg" {
			out = append(out, c)
		}
	}
	return out
}

// A folder converts into a folder of the same shape under the destination,
// with only its audio in it; a file inside it already in the format, written
// in place, is skipped rather than refused, and undo gets the new folder whole.
func TestConvertWalksFolders(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	album := filepath.Join(src, "album")
	for _, n := range []string{"01.mp3", "cd2/02.flac", "cover.jpg", "._01.mp3", ".hidden/03.mp3"} {
		p := filepath.Join(album, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	job := Job{Op: OpConvert, Srcs: []string{album}, Dest: dst,
		Convert: ConvertOpts{Format: AudioOgg, Quality: 1, Encoder: "libvorbis"}}

	f := &fakeTools{}
	res := runConvert(t, f, job, ConflictAsk)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	for _, want := range []string{"album/01.ogg", "album/cd2/02.ogg"} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("%s missing: %v", want, err)
		}
	}
	if n := len(ffmpegRuns(f)); n != 2 {
		t.Errorf("ran ffmpeg %d times, want 2 (no cover art, no dotfiles)", n)
	}
	if want := []string{filepath.Join(dst, "album")}; !slices.Equal(res.Created, want) {
		t.Errorf("created %v, want the folder alone %v", res.Created, want)
	}

	// In place, as mp3: 01.mp3 is already there and is skipped, not an error.
	job.Dest, job.Convert = src, ConvertOpts{Format: AudioMP3, Quality: 1, Encoder: "libmp3lame"}
	f = &fakeTools{}
	if res := runConvert(t, f, job, ConflictSkip); res.Err != nil {
		t.Fatal(res.Err)
	}
	if runs := ffmpegRuns(f); len(runs) != 1 || !strings.Contains(argsOf(runs[0]), "02.flac") {
		t.Errorf("ffmpeg ran %d times, want once for 02.flac", len(runs))
	}
}

func TestConvertEmptyFolder(t *testing.T) {
	src := t.TempDir()
	job := Job{Op: OpConvert, Srcs: []string{src}, Dest: t.TempDir(),
		Convert: ConvertOpts{Format: AudioOgg, Encoder: "libvorbis"}}
	if res := runConvert(t, &fakeTools{}, job, ConflictAsk); res.Err == nil {
		t.Error("an empty folder converted without complaint")
	}
}

// ffmpeg's -progress position, against ffprobe's length, moves the bar for
// the file without counting a step.
func TestConvertReportsPartProgress(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("probeLength only runs ffprobe when it is installed")
	}
	job, _ := convertJob(t, "a.mp3")
	f := &fakeTools{stdout: "100.0\n", lines: []string{"out_time_us=N/A", "out_time_us=25000000", "progress=continue"}}

	ch := make(chan Event)
	ctx := context.Background()
	r := &reporter{ctx: ctx, ch: ch, tools: f}
	go func() {
		_, _, _ = convert(ctx, job, r, newResolver(ctx, ch, ConflictAsk))
		close(ch)
	}()
	var parts []Progress
	for ev := range ch {
		if p, ok := ev.(Progress); ok && p.PartLabel != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) != 1 || parts[0].Part != 0.25 || parts[0].PartLabel != "0:25 / 1:40" || parts[0].Done != 0 {
		t.Errorf("parts = %+v, want one at 25%% of the first file", parts)
	}
}

func TestClock(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0:00", 67 * time.Second: "1:07", 3*time.Hour + 2*time.Minute + 5*time.Second: "3:02:05",
	} {
		if got := clock(d); got != want {
			t.Errorf("clock(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestFFmpegProgressLinesAreNoReason(t *testing.T) {
	lines := []string{"Conversion failed!", "out_time_us=100", "progress=end"}
	if got := lastMeaningful(lines); got != "Conversion failed!" {
		t.Errorf("got %q", got)
	}
}

// The real thing, where ffmpeg is installed: an mp3 in, a playable file out.
func TestConvertWithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "tone.wav")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot make a test tone: %v %s", err, out)
	}

	out := t.TempDir()
	formats, err := AvailableAudioFormats()
	if err != nil {
		t.Fatal(err)
	}
	if len(formats) == 0 {
		t.Fatal("ffmpeg is installed but no format was found for it")
	}
	for _, i := range formats {
		t.Run(i.Label, func(t *testing.T) {
			if i.Format == AudioWAV {
				t.Skip("source is already wav")
			}
			job := Job{Op: OpConvert, Srcs: []string{src}, Dest: out, Convert: ConvertOptsFor(i, i.DefQuality)}
			var (
				res    Result
				parted bool
			)
			for ev := range Run(context.Background(), job) {
				switch ev := ev.(type) {
				case Result:
					res = ev
				case Progress:
					parted = parted || ev.PartLabel != ""
				}
			}
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if !parted {
				t.Error("ffmpeg never reported a position")
			}
			info, err := os.Stat(filepath.Join(out, "tone"+i.Ext))
			if err != nil || info.Size() == 0 {
				t.Fatalf("no output: %v", err)
			}
		})
	}
}

// ffmpeg's closing lines only restate the failure; the reason is above them.
func TestFFmpegFailureNamesTheReason(t *testing.T) {
	lines := []string{
		"Stream map '' matches no streams.",
		"To ignore this, add a trailing '?' to the map.",
		"Failed to set value '0:a:0' for option 'map': Invalid argument",
		"Error parsing options for output file x.flac.",
		"Error opening output files: Invalid argument",
	}
	if got := lastMeaningful(lines); got != lines[0] {
		t.Errorf("got %q, want %q", got, lines[0])
	}
}
