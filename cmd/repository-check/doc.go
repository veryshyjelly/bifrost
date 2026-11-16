// Command repository-check rejects private runtime paths, symlinks and possible
// credentials in repository content. Its default scan covers tracked and
// unignored files; staged mode inspects index content for added, copied, modified
// and renamed entries. Diagnostics identify paths without printing secret values.
package main
