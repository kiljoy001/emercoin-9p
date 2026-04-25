package emercoin9p

import (
	"context"
	"encoding/json"

	"github.com/knusbaum/go9p/fs"
)

// RPCSend defines the interface for sending RPC requests
type RPCSend interface {
	Send(ctx context.Context, query Query) (json.RawMessage, error)
}

// File aliases the real go9p/fs file interface used by this package.
type File = fs.File

// Dir aliases the real go9p/fs directory interface used by this package.
type Dir = fs.Dir

// FileContent defines the interface for file content management
type FileContent interface {
	Set(value string)
	Get() string
}

// SessionFactory defines the interface for session creation
type SessionFactory interface {
	CreateSession(id, name string) (*UserSession, error)
}

// CommandProcessor defines the interface for command processing
type CommandProcessor interface {
	GetResult(ctx context.Context, timeout interface{}) (*CommandResult, error)
}

// FileNode wraps a go9p/fs file.
type FileNode struct {
	fs.File
}
