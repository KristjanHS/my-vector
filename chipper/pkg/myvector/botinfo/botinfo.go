// Package botinfo reads wire-pod's jdocs/botSdkInfo.json (vars.RobotInfoStore) for fork probes.
// The GUID never leaves the calling process.
package botinfo

import (
	"encoding/json"
	"fmt"
	"os"
)

// Robot is one robots[] entry of botSdkInfo.json.
type Robot struct {
	Esn       string `json:"esn"`
	IPAddress string `json:"ip_address"`
	GUID      string `json:"guid"`
}

// Path is $BOTINFO, else this checkout's chipper/jdocs/botSdkInfo.json.
func Path() string {
	if p := os.Getenv("BOTINFO"); p != "" {
		return p
	}
	return os.ExpandEnv("$HOME/projects/my-vector/chipper/jdocs/botSdkInfo.json")
}

// Load returns the first robot in path; zero robots is an error.
func Load(path string) (Robot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Robot{}, err
	}
	var bi struct {
		Robots []Robot `json:"robots"`
	}
	if err := json.Unmarshal(raw, &bi); err != nil {
		return Robot{}, err
	}
	if len(bi.Robots) == 0 {
		return Robot{}, fmt.Errorf("no robots in %s", path)
	}
	return bi.Robots[0], nil
}
