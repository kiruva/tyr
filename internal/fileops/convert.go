package fileops

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kiruva/tyr/internal/metastrip"
)

// Audio conversion is ffmpeg's job. Which formats tyr can write depends less on
// whether ffmpeg is installed than on which encoders it was built with — the
// Homebrew build, for one, ships without libvorbis — so a format is offered
// when ffmpeg lists an encoder for it, and the dialog names the one it found.
//
// Every source becomes one output beside the others in the destination
// directory, named after the source with the new extension. ffmpeg writes to a
// temporary name first and the result is renamed into place, so a conversion
// that fails or is cancelled never leaves a half-written file behind, and never
// destroys the file it was about to replace.

// AudioFormat is an audio format tyr can convert into.
type AudioFormat int

const (
	AudioOgg AudioFormat = iota
	AudioOpus
	AudioMP3
	AudioM4A
	AudioFLAC
	AudioWAV
)

// AudioQuality is one setting on a format's quality dial.
type AudioQuality struct {
	Label string   // "q5 · ~160k", as shown in the dialog
	Args  []string // what it means to ffmpeg
}

// audioEncoder is one ffmpeg encoder that can produce a format.
type audioEncoder struct {
	name string
	// experimental encoders need -strict experimental and are worth a warning;
	// extra is anything else they need to work at all.
	experimental bool
	extra        []string
}

// AudioFormatInfo describes one format tyr can write.
type AudioFormatInfo struct {
	Format AudioFormat
	Label  string // "ogg", as shown in the dialog
	Ext    string // ".ogg"
	Muxer  string // ffmpeg's -f, so the temporary name need not carry the extension

	Qualities  []AudioQuality
	DefQuality int

	// candidates are the encoders that can write this format, best first.
	candidates []audioEncoder

	// Encoder is the one that will run, as resolved by AvailableAudioFormats,
	// and Experimental says it is ffmpeg's own stopgap rather than the real one.
	Encoder      string
	Experimental bool
	extra        []string
}

// HasQualities reports whether the format has more than one setting to choose.
func (i AudioFormatInfo) HasQualities() bool { return len(i.Qualities) > 1 }

func audioFormats() []AudioFormatInfo {
	return []AudioFormatInfo{
		{
			Format: AudioOgg, Label: "ogg", Ext: ".ogg", Muxer: "ogg",
			Qualities: []AudioQuality{
				{"q3 · ~112k", []string{"-q:a", "3"}},
				{"q5 · ~160k", []string{"-q:a", "5"}},
				{"q7 · ~224k", []string{"-q:a", "7"}},
				{"q9 · ~320k", []string{"-q:a", "9"}},
			},
			DefQuality: 1,
			candidates: []audioEncoder{
				{name: "libvorbis"},
				// ffmpeg's built-in Vorbis encoder is experimental and refuses
				// anything but stereo, so a mono source is upmixed for it.
				{name: "vorbis", experimental: true, extra: []string{"-ac", "2"}},
			},
		},
		{
			Format: AudioOpus, Label: "opus", Ext: ".opus", Muxer: "opus",
			Qualities: []AudioQuality{
				{"96k", []string{"-b:a", "96k"}},
				{"128k", []string{"-b:a", "128k"}},
				{"160k", []string{"-b:a", "160k"}},
				{"256k", []string{"-b:a", "256k"}},
			},
			DefQuality: 1,
			candidates: []audioEncoder{{name: "libopus"}},
		},
		{
			Format: AudioMP3, Label: "mp3", Ext: ".mp3", Muxer: "mp3",
			Qualities: []AudioQuality{
				{"V4 · ~165k", []string{"-q:a", "4"}},
				{"V2 · ~190k", []string{"-q:a", "2"}},
				{"V0 · ~245k", []string{"-q:a", "0"}},
				{"320k CBR", []string{"-b:a", "320k"}},
			},
			DefQuality: 1,
			candidates: []audioEncoder{{name: "libmp3lame"}},
		},
		{
			Format: AudioM4A, Label: "m4a (aac)", Ext: ".m4a", Muxer: "ipod",
			Qualities: []AudioQuality{
				{"128k", []string{"-b:a", "128k"}},
				{"192k", []string{"-b:a", "192k"}},
				{"256k", []string{"-b:a", "256k"}},
			},
			DefQuality: 1,
			candidates: []audioEncoder{{name: "libfdk_aac"}, {name: "aac"}},
		},
		{
			Format: AudioFLAC, Label: "flac", Ext: ".flac", Muxer: "flac",
			Qualities:  []AudioQuality{{"lossless", []string{"-compression_level", "5"}}},
			candidates: []audioEncoder{{name: "flac"}},
		},
		{
			Format: AudioWAV, Label: "wav", Ext: ".wav", Muxer: "wav",
			Qualities:  []AudioQuality{{"16-bit PCM", nil}},
			candidates: []audioEncoder{{name: "pcm_s16le"}},
		},
	}
}

// Info returns the description of a format, without any encoder resolved.
func (f AudioFormat) Info() AudioFormatInfo {
	for _, i := range audioFormats() {
		if i.Format == f {
			return i
		}
	}
	return AudioFormatInfo{Format: f, Label: "?"}
}

// ErrNoFFmpeg is what AvailableAudioFormats says when ffmpeg is not on PATH.
var ErrNoFFmpeg = errors.New("ffmpeg is not installed")

// AvailableAudioFormats returns the formats the installed ffmpeg can write,
// each with the encoder that will write it. It runs `ffmpeg -encoders`, which
// takes a few tens of milliseconds. An ffmpeg that is on PATH but will not run
// is its own error, and worth telling apart from a missing one: a package
// manager that upgrades a library under ffmpeg without rebuilding it leaves
// exactly that behind.
func AvailableAudioFormats() ([]AudioFormatInfo, error) {
	if tool("ffmpeg") == "" {
		return nil, ErrNoFFmpeg
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if line := firstLine(exit.Stderr); line != "" {
				return nil, fmt.Errorf("ffmpeg will not run: %s", truncErr(line))
			}
		}
		return nil, fmt.Errorf("ffmpeg will not run: %w", err)
	}
	return availableAudioFormats(parseEncoders(out)), nil
}

func firstLine(b []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(line)
}

func availableAudioFormats(have map[string]bool) []AudioFormatInfo {
	var out []AudioFormatInfo
	for _, i := range audioFormats() {
		for _, c := range i.candidates {
			if !have[c.name] {
				continue
			}
			i.Encoder, i.Experimental, i.extra = c.name, c.experimental, c.extra
			out = append(out, i)
			break
		}
	}
	return out
}

// parseEncoders reads `ffmpeg -encoders`: after a legend ending in " ------",
// one line per encoder, a flags column whose first letter is A for audio, then
// the encoder's name.
func parseEncoders(out []byte) map[string]bool {
	have := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	listing := false
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if !listing {
			listing = len(f) == 1 && strings.HasPrefix(f[0], "---")
			continue
		}
		if len(f) >= 2 && f[0][0] == 'A' {
			have[f[1]] = true
		}
	}
	return have
}

// audioExts are the files worth handing ffmpeg: audio, and the video containers
// whose soundtrack is as often what someone wants out of them.
var audioExts = map[string]bool{
	".mp3": true, ".ogg": true, ".oga": true, ".opus": true, ".flac": true,
	".wav": true, ".m4a": true, ".aac": true, ".wma": true, ".aif": true,
	".aiff": true, ".ape": true, ".wv": true, ".mka": true, ".alac": true,
	".mp4": true, ".m4v": true, ".mkv": true, ".mov": true, ".webm": true,
	".avi": true,
}

// IsAudio reports whether name looks like something ffmpeg can take audio from.
func IsAudio(name string) bool { return audioExts[strings.ToLower(filepath.Ext(name))] }

// ConvertOpts is how OpConvert writes each output. Audio is converted when an
// Encoder is set, images when Images is; a selection holding both kinds gets
// both, and a kind left out is passed over.
type ConvertOpts struct {
	Format  AudioFormat
	Quality int // index into the format's Qualities

	// Encoder is the ffmpeg encoder to use, as resolved by AvailableAudioFormats.
	Encoder string

	// Images turns each image into a clean copy, keeping what Strip says to.
	Images bool
	Strip  metastrip.Options
}

// ImageSummary is the one-line description of what happens to images, for the
// confirm prompt.
func (o ConvertOpts) ImageSummary() string {
	s := "images · strip metadata"
	if o.Strip.KeepOrientation {
		s += " · keep orientation"
	}
	if o.Strip.KeepICC {
		s += " · keep ICC"
	}
	return s
}

// ConvertOptsFor is the options for a resolved format at a quality.
func ConvertOptsFor(i AudioFormatInfo, quality int) ConvertOpts {
	return ConvertOpts{Format: i.Format, Quality: quality, Encoder: i.Encoder}
}

// Summary is the one-line description of the settings, for the confirm prompt.
func (o ConvertOpts) Summary() string {
	i := o.Format.Info()
	s := i.Label
	if q, ok := o.quality(i); ok {
		s += " · " + q.Label
	}
	s += " · " + o.Encoder
	for _, c := range i.candidates {
		if c.name == o.Encoder && c.experimental {
			s += " (experimental)"
		}
	}
	return s
}

func (o ConvertOpts) quality(i AudioFormatInfo) (AudioQuality, bool) {
	if o.Quality < 0 || o.Quality >= len(i.Qualities) {
		return AudioQuality{}, false
	}
	return i.Qualities[o.Quality], true
}

// ConvertTarget is where src lands when converted into dir as format f: the
// same name, the format's extension.
func ConvertTarget(src, dir string, f AudioFormat) string {
	base := filepath.Base(src)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(dir, stem+f.Info().Ext)
}

// OverwritesSource reports whether writing dst would replace src itself. Names
// alone do not settle it: on a case-insensitive disk "Song.MP3" converted to
// mp3 lands on "Song.mp3", which is the same file.
func OverwritesSource(src, dst string) bool {
	if src == dst {
		return true
	}
	a, err := os.Stat(src)
	if err != nil {
		return false
	}
	b, err := os.Stat(dst)
	return err == nil && os.SameFile(a, b)
}

// isFFmpegSummary recognises the lines ffmpeg closes a failure with, which
// restate that something failed without saying what; the line before them is
// the one that does.
func isFFmpegSummary(l string) bool {
	for _, p := range []string{
		"Error opening input files", "Error opening output files",
		"Error parsing options", "Failed to set value", "To ignore this",
	} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// ffmpegArgs builds the command line for one conversion. -nostdin keeps ffmpeg
// from reading keystrokes meant for tyr, -progress reports the position on
// stdout for the bar, and -map 0:a:0 takes the first audio
// stream only, so cover art and video are left behind and a file with no audio
// at all fails with ffmpeg saying so.
func ffmpegArgs(src, tmp string, o ConvertOpts) []string {
	i := o.Format.Info()
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-progress", "pipe:1", "-nostats",
		"-i", argPath(src),
		"-map", "0:a:0", "-map_metadata", "0",
		"-c:a", o.Encoder,
	}
	for _, c := range i.candidates {
		if c.name != o.Encoder {
			continue
		}
		if c.experimental {
			args = append(args, "-strict", "experimental")
		}
		args = append(args, c.extra...)
	}
	if q, ok := o.quality(i); ok {
		args = append(args, q.Args...)
	}
	return append(args, "-f", i.Muxer, argPath(tmp))
}

// convertItem is one file a convert job will write: where it comes from,
// where it goes, and what the progress dialog calls it.
type convertItem struct {
	src, dst string
	label    string
	image    bool // a clean copy rather than an ffmpeg run
	// walked is set for a file found inside a selected folder, as opposed to
	// one that was selected by name.
	walked bool
}

// convertItems expands the selection into the files to convert. A selected
// file converts into Dest; a selected folder is walked, and every audio file
// and image in it lands at the same place under Dest/<folder>, so an album
// converts into an album. Dotfiles are left out of a walk — macOS scatters
// "._song.mp3" resource forks through copied folders, and none of them is
// audio.
func convertItems(ctx context.Context, job Job) ([]convertItem, error) {
	f := job.Convert.Format
	audio, images := job.Convert.Encoder != "", job.Convert.Images
	target := func(path, dir string) (string, bool, bool) {
		switch {
		case audio && IsAudio(path):
			return ConvertTarget(path, dir, f), false, true
		case images && IsImage(path):
			return ImageTarget(path, dir), true, true
		}
		return "", false, false
	}
	var items []convertItem
	for _, src := range job.Srcs {
		info, err := os.Stat(src)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			dst, image, ok := target(src, job.Dest)
			if !ok {
				// Picked by name but of a kind this job leaves out: audio
				// without an ffmpeg to convert it, say.
				continue
			}
			items = append(items, convertItem{
				src: src, dst: dst, label: filepath.Base(src), image: image,
			})
			continue
		}
		parent := filepath.Dir(src)
		err = filepath.WalkDir(src, func(path string, d iofs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path != src && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(parent, path)
			if err != nil {
				return err
			}
			dst, image, ok := target(path, filepath.Join(job.Dest, filepath.Dir(rel)))
			if !ok {
				return nil
			}
			items = append(items, convertItem{
				src:    path,
				dst:    dst,
				label:  filepath.ToSlash(rel),
				image:  image,
				walked: true,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

// convert turns every audio file and image in job.Srcs into one in job.Dest,
// and returns what it created for the undo history, and notes worth showing
// once it is done.
func convert(ctx context.Context, job Job, r *reporter, rv *resolver) ([]string, []string, error) {
	if job.Convert.Encoder == "" && !job.Convert.Images {
		return nil, nil, fmt.Errorf("no encoder chosen")
	}
	items, err := convertItems(ctx, job)
	if err != nil {
		return nil, nil, err
	}
	if len(items) == 0 {
		if !job.Convert.Images {
			return nil, nil, fmt.Errorf("no audio files in the selection")
		}
		return nil, nil, fmt.Errorf("nothing to convert in the selection")
	}
	r.total = len(items)

	// As with a copy, only what is new goes to the undo history: removing a file
	// that replaced one already there would lose both. A folder the job brought
	// into being is recorded whole, so undo takes the folder and not just the
	// files, and nothing inside it is recorded twice.
	var created, newRoots []string
	for _, src := range job.Srcs {
		if root := filepath.Join(job.Dest, filepath.Base(src)); isDir(src) && !exists(root) {
			newRoots = append(newRoots, root)
		}
	}
	underNewRoot := func(p string) bool {
		for _, root := range newRoots {
			if strings.HasPrefix(p, root+string(os.PathSeparator)) {
				return true
			}
		}
		return false
	}
	var tally stripTally
	// finish adds the new folders that did get made, however far the job got.
	finish := func(err error) ([]string, []string, error) {
		for _, root := range newRoots {
			if exists(root) {
				created = append(created, root)
			}
		}
		return created, tally.notes(), err
	}

	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		existed := exists(it.dst)
		var out string
		if it.image {
			out, err = stripOne(it, job, rv, &tally)
		} else {
			out, err = convertOne(ctx, it, job, r, rv)
		}
		if err != nil {
			return finish(err)
		}
		if out != "" && (out != it.dst || !existed) && !underNewRoot(out) {
			created = append(created, out)
		}
		r.step(it.label)
	}
	return finish(nil)
}

// convertOne converts one file, returning where it landed or "" when it was
// skipped: the answer to a name already taken, or a file found in a folder
// that is already in the format and would be written over itself.
func convertOne(ctx context.Context, it convertItem, job Job, r *reporter, rv *resolver) (string, error) {
	dst := it.dst
	if OverwritesSource(it.src, dst) {
		if it.walked {
			return "", nil
		}
		return "", fmt.Errorf("%s is already %s", it.label, job.Convert.Format.Info().Label)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if exists(dst) {
		action, err := rv.resolve(it.src, dst)
		if err != nil {
			return "", err
		}
		switch action {
		case ConflictSkip:
			return "", nil
		case ConflictKeepBoth:
			if dst, err = freeName(dst); err != nil {
				return "", err
			}
		}
	}

	r.part(it.label, 0, "")
	length := probeLength(ctx, it.src, r)

	tmpName, err := tempBeside(dst, 0o644)
	if err != nil {
		return "", err
	}

	cmd := toolCmd{bin: "ffmpeg", args: ffmpegArgs(it.src, tmpName, job.Convert)}
	if err := r.tools.runTool(ctx, cmd, r, ffmpegProgress(it.label, length, r)); err != nil {
		_ = os.Remove(tmpName)
		if strings.Contains(err.Error(), "matches no streams") {
			return "", fmt.Errorf("%s has no audio in it", it.label)
		}
		return "", fmt.Errorf("%s: %w", it.label, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return dst, nil
}

// probeLength asks ffprobe how long src plays, which is what turns ffmpeg's
// running position into a fraction. Zero means unknown — no ffprobe, or a
// stream that does not say — and the bar for the file then shows the position
// alone.
func probeLength(ctx context.Context, src string, r *reporter) time.Duration {
	if tool("ffprobe") == "" {
		return 0
	}
	var out bytes.Buffer
	cmd := toolCmd{bin: "ffprobe", stdout: &out, args: []string{
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", argPath(src),
	}}
	if err := r.tools.runTool(ctx, cmd, r, func(string) string { return "" }); err != nil {
		return 0
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(out.String()), 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// ffmpegProgress reads what -progress writes: a block of key=value lines about
// every half second, of which out_time_us is how far into the audio it is.
// Each one moves the bar for the current file; none of them is a whole step.
func ffmpegProgress(label string, length time.Duration, r *reporter) func(string) string {
	return func(line string) string {
		v, ok := strings.CutPrefix(line, "out_time_us=")
		if !ok {
			return ""
		}
		us, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || us < 0 {
			return ""
		}
		at := time.Duration(us) * time.Microsecond
		if length <= 0 {
			r.part(label, 0, clock(at))
			return ""
		}
		r.part(label, min(float64(at)/float64(length), 1), clock(at)+" / "+clock(length))
		return ""
	}
}

// clock renders a position the way a player shows it: 3:07, or 1:02:45.
func clock(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// isFFmpegProgress recognises a -progress line, which is never the reason a
// conversion failed.
func isFFmpegProgress(l string) bool {
	k, _, ok := strings.Cut(l, "=")
	return ok && k != "" && !strings.ContainsAny(k, " \t") && strings.ToLower(k) == k
}
