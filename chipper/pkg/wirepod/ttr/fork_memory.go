package wirepod_ttr

import (
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
)

// forkMemory is esn's {facts} and {diary} for utterance on brain; a var so tests fake it.
var forkMemory = func(esn, utterance, brain string) (facts, diary string) {
	s, err := memory.Read()
	if err != nil {
		logger.Println("myvector: memory read:", err)
		return "", ""
	}
	return memory.Block(s, esn, utterance, brain)
}
