package config

import (
	"os"
	"strings"
)

// Layer is one rung of the five (C2, D2). Order is the whole of C2: a user
// configures once; a project overrides what is project-specific; a flag wins
// for one run. A later layer beats an earlier one.
type Layer int

const (
	LayerDefaults Layer = iota // built-in defaults — the floor
	LayerUser                 // the user's file (~/.config/aeon/config.yaml)
	LayerProject              // the project's file (./aeon/config.yaml)
	LayerEnv                  // the environment
	LayerFlags                // the flags — win for one run
)

func (l Layer) String() string {
	switch l {
	case LayerDefaults:
		return "defaults"
	case LayerUser:
		return "user file"
	case LayerProject:
		return "project file"
	case LayerEnv:
		return "environment"
	case LayerFlags:
		return "flags"
	}
	return "?"
}

// File is the two file layers' location, named so validation can say which
// file a key was read from (D3, C3): with the same key in three layers, the
// name is the fix, not "invalid configuration".
type File struct {
	Path string
}

func (f File) Name() string { return f.Path }

// Config is the merged five layers. A key's value is the last layer that set
// it; the layer that set it is remembered so validation can name the file
// that owns it.
type Config struct {
	values map[string]string
	// layers records, per key, which layer set it last — so a validation can
	// name the file that owns the key (D3). A key with no file layer set it
	// has an empty File (an environment or flag value).
	layers map[string]File
	Files  []File // the files that were read, in layer order
}

// Load merges the five layers in order (C2, D2) and refuses any credential
// in a config file (C4, a load-time error). defaults is the floor; user and
// project are the two files; env and flags win over everything before them.
// A credential-shaped key in a file is a load-time error, not a warning.
func Load(defaults map[string]string, user, project File, env, flags map[string]string) (Config, error) {
	c := Config{values: map[string]string{}, layers: map[string]File{}}
	c.apply(LayerDefaults, defaults, File{})
	c.apply(LayerUser, fileValues(user), user)
	if err := c.refuseCredentialsIn(user); err != nil {
		return Config{}, err
	}
	c.apply(LayerProject, fileValues(project), project)
	if err := c.refuseCredentialsIn(project); err != nil {
		return Config{}, err
	}
	c.apply(LayerEnv, env, File{})
	c.apply(LayerFlags, flags, File{})
	c.Files = append(c.Files, user, project)
	return c, nil
}

// apply folds one layer over the merged values: a key the layer sets wins
// over what came before, and the layer that set it last is remembered.
func (c *Config) apply(layer Layer, vals map[string]string, f File) {
	for k, v := range vals {
		if v == "" {
			continue // an empty layer value does not set a key
		}
		c.values[k] = v
		c.layers[k] = f
	}
}

// refuseCredentialsIn is the load-time rule C4 makes non-negotiable: a
// credential-shaped key in a config file is a permanent, public leak, so it
// fails the load — not a warning. The error names the key and the file (the
// fix), not "invalid configuration" (a support ticket).
func (c *Config) refuseCredentialsIn(f File) error {
	if f.Path == "" {
		return nil
	}
	for k := range fileValues(f) {
		if isCredentialName(k) {
			return NewCredentialInConfig(k, f.Path)
		}
	}
	return nil
}

// Value reads a key's merged value. An absent key is (zero, false), never an
// error — absence is a normal answer, not a fault (DB19).
func (c Config) Value(key string) (string, bool) {
	v, ok := c.values[key]
	return v, ok
}

// fileValues reads a file layer. A missing file is not a fault (a project
// with no config is normal); a present file is read. YAML parsing is
// deferred to the caller's loader; here a file is its key/value map.
func fileValues(f File) map[string]string {
	if f.Path == "" {
		return nil
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil // a missing file is a normal absence, not a load fault
	}
	return parseKV(string(b))
}

// parseKV is the file format config files use: one key per line,
// key: value. It is deliberately small; the five-layer merge is the point,
// not a YAML engine.
func parseKV(s string) map[string]string {
	out := map[string]string{}
	for _, line := range splitLines(s) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// quote renders a key or a file for the message, naming the thing.
func quote(s string) string { return "\"" + s + "\"" }

// splitLines splits on newlines; an empty string is no lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, l)
	}
	return out
}
