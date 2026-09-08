package engine

import (
	"github.com/bartkleypas/please/internal/storage"
)

// RemoteDaemonStorage implements Storage by proxying node mutations to a Please daemon (forwarded from internal/storage)
type RemoteDaemonStorage = storage.RemoteDaemonStorage

// NewRemoteDaemonStorage initializes a storage instance connected to the Please engine daemon
var NewRemoteDaemonStorage = storage.NewRemoteDaemonStorage
