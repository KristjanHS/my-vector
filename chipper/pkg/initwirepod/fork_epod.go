package initwirepod

import (
	"os"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/epod"
)

// forkEpodCert is StartChipper's non-android Escape Pod pair (T2); a half pair exits.
func forkEpodCert() (crt, key []byte) {
	crt, key, own, err := epod.Pair()
	if err != nil {
		logger.Println(err)
		os.Exit(1)
	}
	if own {
		logger.Println("my-vector: serving our own escapepod cert from " + epod.Dir())
	}
	return crt, key
}
