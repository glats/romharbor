package romsource

import (
	"encoding/json"
	"errors"
	"strings"

	"app.ddd/internal/adapter/romsource/archiveorgapi"
)

type legacyLabelError struct {
	message string
	err     error
}

func (e legacyLabelError) Error() string { return e.message }

func (e legacyLabelError) Unwrap() error { return e.err }

var legacyDecodeLabels = strings.NewReplacer(
	"archiveorgapi.ItemMetadata", "romsource.itemMetadata",
	"Go struct field ItemMetadata.", "Go struct field itemMetadata.",
	"archiveorgapi.ItemFile", "romsource.itemFile",
	"Go struct field ItemFile.", "Go struct field itemFile.",
	"dlarchiveapi.DlArchiveItem", "romsource.dlArchiveItem",
	"Go struct field DlArchiveItem.", "Go struct field dlArchiveItem.",
	"Go struct field DlArchiveListResponse.", "Go struct field .",
	"Go struct field DlArchiveFileURLResponse.", "Go struct field .",
	"dlarchiveapi.DlArchiveListResponse", `struct { OK bool "json:\"ok\""; Items []romsource.dlArchiveItem "json:\"items\""; Total int "json:\"total\"" }`,
	"dlarchiveapi.DlArchiveFileURLResponse", `struct { OK bool "json:\"ok\""; URL string "json:\"url\"" }`,
)

func legacyDecodeError(err error) error {
	if err == nil {
		return nil
	}
	message := legacyDecodeLabels.Replace(err.Error())
	if message == err.Error() {
		return err
	}
	return legacyLabelError{message: message, err: err}
}

func legacyFileSizeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	legacy := &json.UnmarshalTypeError{Value: typeErr.Value, Type: typeErr.Type, Struct: "itemFile", Field: "files.size"}
	return legacyLabelError{message: legacy.Error(), err: err}
}

func legacyArchiveDecodeError(err error, meta *archiveorgapi.ItemMetadata) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		for _, file := range meta.Files {
			var size sizeValue
			if sizeErr := size.UnmarshalJSON(file.Size); sizeErr != nil {
				return legacyFileSizeError(sizeErr)
			}
		}
	}
	return legacyDecodeError(err)
}
