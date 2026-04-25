package emercoin9p

import (
	"fmt"
	"sync"

	dp9ikauth "github.com/kiljoy001/go-dp9ik/p9auth"
	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// UserSession represents a user session in the 9P namespace.
type UserSession struct {
	ID        string
	Name      string
	Ctl       *CommandFile
	Data      *DataFile
	Status    *StatusFile
	Namespace *Namespace
	state     *sessionState
}

// GetData returns the session data file content.
func (s *UserSession) GetData() string {
	return s.Data.GetData()
}

// GetStatus returns the session status file content.
func (s *UserSession) GetStatus() string {
	return s.Status.GetStatus()
}

// SetStatus updates the session status file content.
func (s *UserSession) SetStatus(status string) {
	s.Status.SetStatus(status)
}

// Namespace represents a 9P namespace backed by go9p/fs.
type Namespace struct {
	fsys        *fs.FS
	root        *fs.StaticDir
	control     *UserSession
	sessionsDir *fs.StaticDir
	sessions    map[string]*UserSession
	config      namespaceConfig
	mu          sync.RWMutex
}

func newNamespace(withSessions bool, config namespaceConfig) *Namespace {
	fsOpts := make([]fs.Option, 0, 1)
	if config.auth != nil {
		fsOpts = append(fsOpts, fs.WithAuth(dp9ikauth.AuthFunc(dp9ikauth.Config{
			Domain:   config.auth.Domain,
			User:     config.auth.User,
			Password: config.auth.Password,
		})))
	}

	fsys, root := fs.NewFS("emercoin", "emercoin", 0755, fsOpts...)
	ns := &Namespace{
		fsys:     fsys,
		root:     root,
		sessions: make(map[string]*UserSession),
		config:   config,
	}

	if withSessions {
		controlState := newSessionState(config)
		ctl, data, status, err := addSessionFiles(fsys, root, "emercoin", controlState)
		if err != nil {
			panic(err)
		}
		ns.control = &UserSession{
			ID:        "root",
			Name:      "/",
			Ctl:       ctl,
			Data:      data,
			Status:    status,
			Namespace: ns,
			state:     controlState,
		}

		sessionsDir := fs.NewStaticDir(fsys.NewStat("sessions", "emercoin", "emercoin", 0755|proto.DMDIR))
		if err := root.AddChild(sessionsDir); err != nil {
			panic(err)
		}
		ns.sessionsDir = sessionsDir
	}

	return ns
}

// NewNs creates a new namespace rooted at a real go9p filesystem.
func NewNs(opts ...NamespaceOption) *Namespace {
	return newNamespace(true, makeNamespaceConfig(opts))
}

// FS returns the underlying go9p filesystem.
func (ns *Namespace) FS() *fs.FS {
	return ns.fsys
}

// Root returns the namespace root directory.
func (ns *Namespace) Root() *fs.StaticDir {
	return ns.root
}

// Control returns the root control session for the namespace.
func (ns *Namespace) Control() *UserSession {
	return ns.control
}

// Server returns a go9p server for the namespace.
func (ns *Namespace) Server() go9p.Srv {
	return ns.fsys.Server()
}

// CreateSession creates a new isolated session namespace and exposes it under /sessions/<name>.
func (ns *Namespace) CreateSession(id, name string) (*UserSession, error) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	return ns.createSessionLocked(id, name, newSessionState(ns.config))
}

// CloneSession copies an existing session into a new isolated namespace.
func (ns *Namespace) CloneSession(sourceID, newID, newName string) (*UserSession, error) {
	ns.mu.RLock()
	source, ok := ns.sessions[sourceID]
	ns.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("source session not found: %s", sourceID)
	}

	ns.mu.Lock()
	defer ns.mu.Unlock()

	return ns.createSessionLocked(newID, newName, source.state.clone())
}

func (ns *Namespace) createSessionLocked(id, name string, state *sessionState) (*UserSession, error) {
	if ns.sessionsDir == nil {
		return nil, fmt.Errorf("session management is not available in this namespace")
	}
	if _, exists := ns.sessions[id]; exists {
		return nil, fmt.Errorf("session already exists: %s", id)
	}
	if _, exists := ns.sessionsDir.Children()[name]; exists {
		return nil, fmt.Errorf("session directory already exists: %s", name)
	}

	sessionNS, ctl, data, status, err := newSessionNamespace(name, state)
	if err != nil {
		return nil, err
	}

	sessionDir := fs.NewStaticDir(ns.fsys.NewStat(name, name, name, 0755|proto.DMDIR))
	if _, _, _, err := addSessionFiles(ns.fsys, sessionDir, name, state); err != nil {
		return nil, err
	}
	if err := ns.sessionsDir.AddChild(sessionDir); err != nil {
		return nil, err
	}

	session := &UserSession{
		ID:        id,
		Name:      name,
		Ctl:       ctl,
		Data:      data,
		Status:    status,
		Namespace: sessionNS,
		state:     state,
	}
	ns.sessions[id] = session
	return session, nil
}

func newSessionNamespace(owner string, state *sessionState) (*Namespace, *CommandFile, *DataFile, *StatusFile, error) {
	ns := newNamespace(false, defaultNamespaceConfig())
	ctl, data, status, err := addSessionFiles(ns.fsys, ns.root, owner, state)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return ns, ctl, data, status, nil
}

func addSessionFiles(fileFS *fs.FS, parent *fs.StaticDir, owner string, state *sessionState) (*CommandFile, *DataFile, *StatusFile, error) {
	ctl := NewCommandFile(fileFS.NewStat("ctl", owner, owner, 0666), state)
	data := NewDataFile(fileFS.NewStat("data", owner, owner, 0444), state)
	status := NewStatusFile(fileFS.NewStat("status", owner, owner, 0444), state)

	for _, node := range []fs.FSNode{ctl, data, status} {
		if err := parent.AddChild(node); err != nil {
			return nil, nil, nil, err
		}
	}

	return ctl, data, status, nil
}
