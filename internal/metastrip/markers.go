package metastrip

import (
	"fmt"
	"slices"
	"strings"
)

// Provenance and AI-generation markers found in decoded metadata. These only
// cover what metadata declares; watermarks hidden in the pixels themselves
// (SynthID, Stable Signature, TrustMark, Digimarc...) are not detectable here.

var digitalSourceTypes = map[string]string{
	"trainedAlgorithmicMedia":              "AI-generated",
	"compositeWithTrainedAlgorithmicMedia": "composite including AI-generated content",
	"algorithmicallyEnhanced":              "algorithmically enhanced",
	"algorithmicMedia":                     "algorithmically generated (not AI-trained)",
	"compositeSynthetic":                   "composite with synthetic elements",
	"digitalCapture":                       "captured by camera",
	"computationalCapture":                 "computational camera capture",
	"negativeFilm":                         "scanned from negative film",
	"positiveFilm":                         "scanned from positive film",
	"print":                                "scanned from print",
	"digitalArt":                           "digital art made by a human",
	"virtualRecording":                     "virtual recording",
	"screenCapture":                        "screen capture",
	"dataDrivenMedia":                      "data-driven media",
}

// PNG text keywords written by image generation tools.
var generatorKeywords = map[string]string{
	"parameters":        "Stable Diffusion WebUI (A1111/Forge) generation parameters",
	"prompt":            "ComfyUI prompt graph",
	"workflow":          "ComfyUI workflow",
	"invokeai_metadata": "InvokeAI generation metadata",
	"sd-metadata":       "InvokeAI generation metadata",
	"Dream":             "InvokeAI/Dream prompt",
	"generation_data":   "AI generation data",
}

// Names of image generators, matched case-insensitively in any value.
var generatorNames = []string{
	"midjourney", "dall-e", "dall·e", "stable diffusion", "stability ai", "adobe firefly",
	"made with google ai", "novelai", "openai", "chatgpt", "gpt-4o", "comfyui",
	"automatic1111", "invokeai", "leonardo.ai", "ideogram", "black forest labs", "flux.1",
	"bing image creator", "microsoft designer", "meta ai",
}

// Generator names that are also ordinary words — "imagen" is Spanish for
// image — and so count only in a field that names the software behind the
// file, never in a caption.
var generatorToolNames = []string{"imagen", "grok"}

// toolField reports whether a field names the program that made the file.
func toolField(key string) bool {
	for _, k := range []string{"software", "creatortool", "generator", "softwareagent", "originatingprogram"} {
		if strings.Contains(key, k) {
			return true
		}
	}
	return false
}

type itemFields struct {
	item   Item
	fields []Field
}

func findMarkers(items []itemFields) []string {
	var out []string
	seen := map[string]bool{}
	add := func(format string, a ...any) {
		if s := fmt.Sprintf(format, a...); !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, it := range items {
		isC2PA := it.item.Kind == "jumbf" && len(it.fields) > 0 &&
			strings.HasPrefix(it.fields[0].Value, "C2PA")
		isPNGText := strings.HasPrefix(it.item.Kind, "png:") && strings.HasSuffix(it.item.Kind, "Xt")
		if isC2PA {
			signer := ""
			for _, f := range it.fields {
				if f.Key == "Signer" {
					signer = f.Value
					break
				}
			}
			if signer != "" {
				add("C2PA Content Credentials present, signed by %s", signer)
			} else {
				add("C2PA Content Credentials present")
			}
		}
		for _, f := range it.fields {
			key := strings.ToLower(f.Key)
			switch {
			case strings.Contains(key, "digitalsourcetype"):
				code := f.Value[strings.LastIndex(f.Value, "/")+1:]
				desc := digitalSourceTypes[code]
				if desc == "" {
					desc = code
				}
				add("digital source type: %s (%s)", desc, f.Key)
			case f.Key == "SoftBinding":
				add("declares an invisible watermark / fingerprint (C2PA soft binding, algorithm %s)", f.Value)
			case key == "dcterms:provenance":
				add("links to a remote C2PA manifest: %s", f.Value)
			case key == "cawg.training-mining" || strings.HasPrefix(key, "c2pa.training-mining"):
				add("AI training/mining preferences declared: %s", f.Value)
			case isPNGText && generatorKeywords[f.Key] != "":
				add("%s (PNG text %q)", generatorKeywords[f.Key], f.Key)
			}
			lv := strings.ToLower(f.Value)
			names := generatorNames
			if toolField(key) {
				names = append(slices.Clip(names), generatorToolNames...)
			}
			for _, g := range names {
				if containsWord(lv, g) {
					add("mentions %q in %s", g, fieldLabel(it.item, f))
				}
			}
		}
	}
	return out
}

func fieldLabel(it Item, f Field) string {
	if f.Key == "" {
		return it.Name
	}
	return f.Key
}

// containsWord reports whether s contains w not embedded in a longer word.
func containsWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(w)
		if (j == 0 || !isWordByte(s[j-1])) && (end == len(s) || !isWordByte(s[end])) {
			return true
		}
		i = j + 1
	}
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z'
}
