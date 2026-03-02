package determinism

import (
	"fmt"
	"time"
)

// FreezeTimeEnv returns the GAUNTLET_FREEZE_TIME environment variable value.
func FreezeTimeEnv(t time.Time) string {
	return fmt.Sprintf("GAUNTLET_FREEZE_TIME=%s", t.Format(time.RFC3339))
}
