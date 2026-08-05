package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/vowifihil"
	"github.com/strongswan/govici/vici"
)

type result struct {
	Success          bool   `json:"success"`
	Stage            string `json:"stage"`
	RequiredPlugins  bool   `json:"requiredPlugins"`
	ConnectionLoaded bool   `json:"connectionLoaded"`
	Initiated        bool   `json:"initiated"`
}

func main() {
	if os.Geteuid() != 0 || len(os.Args) != 1 {
		fail("invocation", result{})
	}
	input, err := readInput()
	if err != nil {
		fail("input", result{})
	}
	session, err := vici.NewSession(vici.WithSocketPath(vowifihil.VICISocket))
	if err != nil {
		fail("connect", result{})
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	state := result{}
	stats, err := session.Call(ctx, "stats", nil)
	if err != nil || !hasRequiredPlugins(stats) {
		fail("plugins", state)
	}
	state.RequiredPlugins = true
	connection, err := vowifihil.ConnectionMessage(input)
	if err != nil {
		fail("connection-encode", state)
	}
	if _, err := session.Call(ctx, "load-conn", connection); err != nil {
		fail("connection-load", state)
	}
	known, err := session.Call(ctx, "get-conns", nil)
	if err != nil || !hasString(known.Get("conns"), vowifihil.ConnectionName) {
		fail("connection-verify", state)
	}
	state.ConnectionLoaded = true

	initiate := vici.NewMessage()
	if initiate.Set("ike", vowifihil.ConnectionName) != nil || initiate.Set("child", "ims") != nil ||
		initiate.Set("timeout", 35000) != nil || initiate.Set("loglevel", 0) != nil {
		fail("initiate-encode", state)
	}
	for _, eventErr := range session.CallStreaming(ctx, "initiate", "control-log", initiate) {
		if eventErr != nil {
			fail("initiate", state)
		}
	}
	state.Success = true
	state.Stage = "complete"
	state.Initiated = true
	writeResult(state)
}

func readInput() (vowifihil.ConnectionInput, error) {
	info, err := os.Lstat(vowifihil.VICIConfig)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return vowifihil.ConnectionInput{}, errors.New("invalid VICI input file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return vowifihil.ConnectionInput{}, errors.New("invalid VICI input owner")
	}
	file, err := os.Open(vowifihil.VICIConfig)
	if err != nil {
		return vowifihil.ConnectionInput{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) == 0 || len(data) > 4096 {
		return vowifihil.ConnectionInput{}, errors.New("invalid VICI input size")
	}
	defer zero(data)
	return vowifihil.ParseConnectionInput(data)
}

func hasRequiredPlugins(message *vici.Message) bool {
	if message == nil {
		return false
	}
	plugins, ok := message.Get("plugins").([]string)
	if !ok {
		return false
	}
	for _, required := range vowifihil.RequiredPlugins() {
		found := false
		for _, plugin := range plugins {
			if plugin == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hasString(value any, expected string) bool {
	values, ok := value.([]string)
	if !ok {
		return false
	}
	for _, candidate := range values {
		if candidate == expected {
			return true
		}
	}
	return false
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func fail(stage string, state result) {
	state.Success = false
	state.Stage = stage
	writeResult(state)
	os.Exit(1)
}

func writeResult(state result) {
	_ = json.NewEncoder(os.Stdout).Encode(state)
}
