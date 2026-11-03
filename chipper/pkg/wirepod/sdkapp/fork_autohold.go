package sdkapp

// my-vector glue: hands sdkapp internals to chipper/pkg/myvector/autohold.

import (
	"context"
	"net"
	"os"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/autohold"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

func InitAutoHold() {
	autohold.Start(os.Getenv("AUTO_HOLD"), autohold.Deps{
		Open:  openRobotHoldStream,
		Touch: resetConnTimer,
		ESNs:  configuredESNs,
	})
}

func configuredESNs() []string {
	var esns []string
	for _, bot := range vars.BotInfo.Robots {
		esns = append(esns, bot.Esn)
	}
	return esns
}

// openRobotHoldStream probes :443 first so an offline robot never reaches newRobot, which leaks a conn per failure.
func openRobotHoldStream(esn string) (autohold.Stream, func(), error) {
	for _, bot := range vars.BotInfo.Robots {
		if strings.EqualFold(bot.Esn, esn) && bot.IPAddress != "" {
			c, err := net.DialTimeout("tcp", bot.IPAddress+":443", 3*time.Second)
			if err != nil {
				return nil, nil, err
			}
			c.Close()
		}
	}
	robot, _, err := getRobot(esn)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(robot.Ctx)
	s, err := robot.Vector.Conn.BehaviorControl(ctx)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return s, cancel, nil
}

func resetConnTimer(esn string) {
	rs := robots // removeRobot may swap in a shorter slice mid-loop
	for i := range rs {
		if strings.EqualFold(rs[i].ESN, esn) {
			rs[i].ConnTimer = 0
		}
	}
}
