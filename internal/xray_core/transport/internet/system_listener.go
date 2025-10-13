package internet

import (
	"github.com/sagernet/sing/common/control"
)

type DefaultListener struct {
	controllers []control.Func
}
