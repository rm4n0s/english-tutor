package menuentry

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// This file is the macOS counterpart of the desktop entry: a small application bundle, `English Tutor.app`, in the user's
// Applications folder (Launchpad, Spotlight and the Dock find it there). Its executable is a shell script that runs the program
// at Entry.Exec, so the bundle stays small and follows the program when it is updated; running -setup-desktop again after the
// program moved fixes the path. Its icon is the 64x64 PNG of the entry in an icns file (the `icp6` chunk holds a PNG).

// Bundle tells where the files of the bundle were written.
type Bundle struct {
	// App is the bundle, Plist its Info.plist, Script the executable inside it and Icon the icns file.
	App, Plist, Script, Icon string
}

// bundleName is the name of the bundle's folder: the entry's name with the characters that a file name cannot hold left out.
func bundleName(e Entry) string {
	name := strings.NewReplacer("/", "", ":", "", "\x00", "").Replace(e.Name)
	return strings.TrimSpace(name) + ".app"
}

// InstallBundle writes the bundle of the entry under dir (the user's Applications folder), replacing the one of an earlier call.
// It is written in a folder of its own and moved into place, so Launchpad never reads half a bundle.
func InstallBundle(dir string, e Entry) (Bundle, error) {
	app := filepath.Join(dir, bundleName(e))
	exeName := e.ID
	b := Bundle{
		App:    app,
		Plist:  filepath.Join(app, "Contents", "Info.plist"),
		Script: filepath.Join(app, "Contents", "MacOS", exeName),
		Icon:   filepath.Join(app, "Contents", "Resources", e.ID+".icns"),
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return Bundle{}, errors.NewErr("BundleDirFailed", err).SetMetadata("path", dir)
	}
	stage, err := os.MkdirTemp(dir, ".english-tutor-bundle-*")
	if err != nil {
		return Bundle{}, errors.NewErr("BundleDirFailed", err).SetMetadata("path", dir)
	}
	staged := Bundle{
		App:    filepath.Join(stage, filepath.Base(app)),
		Plist:  filepath.Join(stage, filepath.Base(app), "Contents", "Info.plist"),
		Script: filepath.Join(stage, filepath.Base(app), "Contents", "MacOS", exeName),
		Icon:   filepath.Join(stage, filepath.Base(app), "Contents", "Resources", e.ID+".icns"),
	}
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{staged.Icon, icns(e.IconPNG), fileMode},
		{staged.Plist, []byte(infoPlist(e, exeName)), fileMode},
		{staged.Script, []byte(launcher(e.Exec)), 0o755},
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.path), dirMode); err != nil {
			return Bundle{}, cleanStage(errors.NewErr("BundleDirFailed", err).SetMetadata("path", filepath.Dir(f.path)), stage)
		}
		if err := os.WriteFile(f.path, f.data, f.mode); err != nil {
			return Bundle{}, cleanStage(errors.NewErr("BundleWriteFailed", err).SetMetadata("path", f.path), stage)
		}
		if err := os.Chmod(f.path, f.mode); err != nil { // WriteFile's mode is cut by the umask
			return Bundle{}, cleanStage(errors.NewErr("BundleWriteFailed", err).SetMetadata("path", f.path), stage)
		}
	}
	if err := os.RemoveAll(app); err != nil {
		return Bundle{}, cleanStage(errors.NewErr("BundleReplaceFailed", err).SetMetadata("path", app), stage)
	}
	if err := os.Rename(staged.App, app); err != nil {
		return Bundle{}, cleanStage(errors.NewErr("BundleReplaceFailed", err).SetMetadata("path", app), stage)
	}
	if err := os.Remove(stage); err != nil {
		return b, errors.NewErr("BundleDirFailed", err).SetMetadata("path", stage)
	}
	return b, nil
}

// cleanStage removes the staging folder after a failure and adds a failure to remove it to the error.
func cleanStage(err error, stage string) error {
	if rerr := os.RemoveAll(stage); rerr != nil {
		if appErr, ok := errors.FromError(err); ok {
			return appErr.SetMetadata("cleanup", rerr.Error())
		}
	}
	return err
}

// launcher is the executable of the bundle: it hands over to the program, with the arguments it was given.
func launcher(exe string) string {
	return "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", `'\''`) + "' \"$@\"\n"
}

// infoPlist is the bundle's Info.plist.
func infoPlist(e Entry, exeName string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, kv := range [][2]string{
		{"CFBundleName", e.Name}, {"CFBundleDisplayName", e.Name}, {"CFBundleIdentifier", e.ID}, {"CFBundleExecutable", exeName},
		{"CFBundleIconFile", e.ID}, {"CFBundlePackageType", "APPL"}, {"CFBundleVersion", "1"}, {"CFBundleShortVersionString", "1"},
		{"LSMinimumSystemVersion", "12.0"}, {"LSApplicationCategoryType", "public.app-category.education"},
		{"NSHumanReadableCopyright", e.Comment},
	} {
		b.WriteString("\t<key>" + xmlEscape(kv[0]) + "</key>\n\t<string>" + xmlEscape(kv[1]) + "</string>\n")
	}
	b.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n</dict>\n</plist>\n")
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// icns wraps a 64x64 PNG in an icns file: the type `icns`, the length of the file, and one chunk of the type `icp6` (64x64,
// PNG) with its own length, all big-endian.
func icns(png []byte) []byte {
	var b bytes.Buffer
	b.WriteString("icns")
	_ = binary.Write(&b, binary.BigEndian, uint32(8+8+len(png))) // writing to a buffer does not fail
	b.WriteString("icp6")
	_ = binary.Write(&b, binary.BigEndian, uint32(8+len(png))) // the same
	b.Write(png)
	return b.Bytes()
}
