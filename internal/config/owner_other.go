//go:build !unix

package config

import "os"

// checkOwner: file ownership is not checked on platforms without Unix ids.
func checkOwner(os.FileInfo, string, bool) error { return nil }
