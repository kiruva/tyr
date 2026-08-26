package fileops

// preferSevenZipForZip is on for Windows. An `unzip` on PATH there is a Unix
// build shipped by Git for Windows or MSYS2, and it does not reliably open an
// archive named by a native path — a `D:\a\...\x.zip` comes back as "cannot
// find or open". 7-Zip is the tool Windows users have for zips anyway, and it
// takes native paths as given. unzip stays as the fallback when 7-Zip is
// missing.
const preferSevenZipForZip = true
