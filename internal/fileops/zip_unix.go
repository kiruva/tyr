//go:build !windows

package fileops

// preferSevenZipForZip reports whether a .zip should go to 7-Zip even when
// Info-ZIP's unzip is installed and the archive is not encrypted. Off here:
// unzip is the native tool, and it reports progress per file.
const preferSevenZipForZip = false
