/* The consistent hash ring from the original fnlb.
   The behaviour of this depends on changes to the runner list leaving it relatively stable.
*/

package agent

import (
	"context"
	"time"

	"github.com/dchest/siphash"
	"github.com/fnproject/fn/api/models"
	"github.com/sirupsen/logrus"
)

type chPlacer struct {
}

func NewCHPlacer() Placer {
	return &chPlacer{}
}

// This borrows the CH placement algorithm from the original FNLB.
// Because we ask a runner to accept load (queuing on the LB rather than on the nodes), we don't use
// the LB_WAIT to drive placement decisions: runners only accept work if they have the capacity for it.
func (p *chPlacer) PlaceCall(np NodePool, ctx context.Context, call *call, lbGroupID string) error {
	// The key is just the path in this case
	key := call.Path
	sum64 := siphash.Hash(0, 0x4c617279426f6174, []byte(key))
	timeout := time.After(call.slotDeadline.Sub(time.Now()))
	for {

		select {
		case <-ctx.Done():
			return models.ErrCallTimeoutServerBusy
		case <-timeout:
			return models.ErrCallTimeoutServerBusy
		default:
			runners := np.Runners(lbGroupID)
			i := int(jumpConsistentHash(sum64, int32(len(runners))))
			for j := 0; j < len(runners); j++ {
				r := runners[i]

				placed, err := r.TryExec(ctx, call)
				if err != nil {
					logrus.WithError(err).Error("Failed during call placement")
				}
				if placed {
					return err
				}

				i = (i + 1) % len(runners)
			}

			remaining := call.slotDeadline.Sub(time.Now())
			if remaining <= 0 {
				return models.ErrCallTimeoutServerBusy
			}
			time.Sleep(minDuration(retryWaitInterval, remaining))
		}
	}
}

// A Fast, Minimal Memory, Consistent Hash Algorithm:
// https://arxiv.org/ftp/arxiv/papers/1406/1406.2294.pdf
func jumpConsistentHash(key uint64, num_buckets int32) int32 {
	var b, j int64 = -1, 0
	for j < int64(num_buckets) {
		b = j
		key = key*2862933555777941757 + 1
		j = (b + 1) * int64((1<<31)/(key>>33)+1)
	}
	return int32(b)
}
