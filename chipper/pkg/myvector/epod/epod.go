// Package epod picks the Escape Pod TLS pair wire-pod serves on :443 (T2): our own
// (robot/epod/mkcert.sh) when EPOD_DIR holds one, else upstream's public ./epod pair.
package epod

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
)

// DefaultDir is EPOD_DIR relative to cwd chipper/; MYVECTOR_EPOD_DIR overrides it.
const DefaultDir = "../secrets/epod"

// UpstreamDir is upstream's tracked pair, whose key is public.
const UpstreamDir = "./epod"

// PublicLog is logged when the fallback serves upstream's public pair.
const PublicLog = "my-vector: serving the PUBLIC escapepod cert (" + UpstreamDir + "); run robot/epod/mkcert.sh"

var logln = logger.Println

// Dir is the EPOD_DIR in effect.
func Dir() string {
	if d := os.Getenv("MYVECTOR_EPOD_DIR"); d != "" {
		return d
	}
	return DefaultDir
}

// Pair returns the cert + key to serve from Dir(), falling back to UpstreamDir.
func Pair() (crt, key []byte, own bool, err error) { return PairFrom(Dir(), UpstreamDir) }

// PairFrom returns dir's ep.{crt,key} with own=true when both exist, fallback's when
// neither does, and an error when only one does (fail closed).
func PairFrom(dir, fallback string) (crt, key []byte, own bool, err error) {
	c, k := filepath.Join(dir, "ep.crt"), filepath.Join(dir, "ep.key")
	_, ce := os.Stat(c)
	_, ke := os.Stat(k)
	switch {
	case ce == nil && ke == nil:
		crt, key, err = read(c, k)
		return crt, key, true, err
	case errors.Is(ce, os.ErrNotExist) && errors.Is(ke, os.ErrNotExist):
		logln(PublicLog)
		crt, key, err = read(filepath.Join(fallback, "ep.crt"), filepath.Join(fallback, "ep.key"))
		return crt, key, false, err
	}
	return nil, nil, false, errors.New("my-vector: " + dir + " holds only half of ep.crt/ep.key; refusing to serve")
}

func read(c, k string) ([]byte, []byte, error) {
	crt, err := os.ReadFile(c)
	if err != nil {
		return nil, nil, err
	}
	key, err := os.ReadFile(k)
	return crt, key, err
}
