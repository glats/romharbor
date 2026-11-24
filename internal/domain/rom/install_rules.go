package rom

import (
	"fmt"
	"strings"
)

// InstallPlan is what the domain decides about a published file before it is
// downloaded.
type InstallPlan struct {
	// Folder is the library folder that receives the file, relative to the
	// library root.
	Folder string
	// Base is the name of the file without its extension.
	Base string
	// Ext is the extension the published file carries.
	Ext string
	// Archive reports whether the file must be opened before the ROM inside
	// it reaches the library.
	Archive bool
	// Size is the published size of the file.
	Size int64
}

// PlanInstall decides where a published file lands in the library of a
// platform and whether it must be opened first.
//
// A file installs under an extension the platform accepts. An archive format
// the platform lists as a ROM extension is the ROM itself and installs as it
// is; any other archive is opened, and the ROM inside it must carry an
// accepted extension too. A file without an extension is refused: the
// library does not guess the extension of a ROM.
func (p Platform) PlanInstall(file File) (InstallPlan, error) {
	base, ext, err := SplitFileName(file.Name)
	if err != nil {
		return InstallPlan{}, err
	}
	folder, err := p.Destination()
	if err != nil {
		return InstallPlan{}, permanent(err)
	}
	plan := InstallPlan{Folder: folder, Base: base, Ext: ext, Size: file.Size}
	switch {
	case ArchiveExtension(ext):
		plan.Archive = !AcceptsExtension(p.Extensions, ext)
		return plan, nil
	case file.Archive:
		return InstallPlan{}, permanent(fmt.Errorf("romcatalog: unsupported archive format for %s", file.Name))
	case ext == "":
		return InstallPlan{}, permanent(fmt.Errorf("romcatalog: %s has no extension to install under", file.Name))
	case !AcceptsExtension(p.Extensions, ext):
		return InstallPlan{}, permanent(fmt.Errorf("romcatalog: %s is not a ROM for the %s library", file.Name, folder))
	}
	return plan, nil
}

// ExistingFile is what the library holds at the place of a planned file.
type ExistingFile struct {
	// Exists reports whether something sits at the place.
	Exists bool
	// Regular reports whether what sits there is a regular file.
	Regular bool
	// Size is the size of the file in bytes.
	Size int64
}

// SkipInstall reports whether the install may be skipped because the library
// already holds the file. An archive is downloaded again: the ROM it holds is
// only known once the archive is open.
func SkipInstall(plan InstallPlan, existing ExistingFile) bool {
	if plan.Archive {
		return false
	}
	if !existing.Exists || !existing.Regular {
		return false
	}
	if plan.Size <= 0 {
		return false
	}
	return existing.Size == plan.Size
}

// SkipSet reports whether a set is already installed because the library
// holds its index.
func SkipSet(index ExistingFile) bool {
	return index.Exists && index.Regular
}

// LibraryPath returns the path of a file or a folder inside the library,
// relative to the library root and separated by slashes. The folder and the
// name must be plain names, so the path always stays inside the library: the
// library root itself, a parent folder and a nested path are refused.
func LibraryPath(folder, name string) (string, error) {
	if !plainName(folder) {
		return "", fmt.Errorf("romcatalog: the library folder %q is not a folder of the library: %w", folder, ErrOutsideLibrary)
	}
	if !plainName(name) {
		return "", fmt.Errorf("romcatalog: the file %q is not a file of the library: %w", name, ErrOutsideLibrary)
	}
	return folder + "/" + name, nil
}

// AcceptsExtension reports whether ext is one of the accepted extensions,
// compared exactly and without case.
func AcceptsExtension(exts []string, ext string) bool {
	for _, e := range exts {
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
}

// plainName reports whether the name is a single element of a path: no
// separator, no parent reference and no drive letter.
func plainName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return len(name) < 2 || name[1] != ':'
}
