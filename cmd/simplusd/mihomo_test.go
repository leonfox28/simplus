package main

import (
	"path/filepath"
	"testing"

	"github.com/leonfox28/simplus/internal/mihomosupervisor"
)

func TestNewMihomoSupervisorSelectsLocalOrSocketImplementationWithoutIO(t *testing.T) {
	root := t.TempDir()
	localAPI, err := newMihomoSupervisor(root, "")
	if err != nil {
		t.Fatal(err)
	}
	local, ok := localAPI.(*mihomosupervisor.Local)
	if !ok || local.Root != filepath.Clean(root) {
		t.Fatalf("local supervisor=%#v", localAPI)
	}

	missingSocket := filepath.Join(t.TempDir(), "mihomo-supervisor.sock")
	clientAPI, err := newMihomoSupervisor(root, missingSocket)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := clientAPI.(*mihomosupervisor.Client); !ok {
		t.Fatalf("socket supervisor=%#v", clientAPI)
	}
}

func TestNewMihomoSupervisorRejectsInvalidPaths(t *testing.T) {
	tests := []struct {
		name       string
		root       string
		socketPath string
	}{
		{name: "local root", root: "relative-root"},
		{name: "socket", root: t.TempDir(), socketPath: "relative.sock"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			supervisor, err := newMihomoSupervisor(test.root, test.socketPath)
			if err == nil || supervisor != nil {
				t.Fatalf("supervisor=%#v err=%v", supervisor, err)
			}
		})
	}
}
