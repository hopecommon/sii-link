package atrust

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/hopecommon/sii-link/internal/ping"
	"github.com/hopecommon/sii-link/log"
)

const pingNum = 3

func getBestNodes(nodeGroups map[string][]string, dialContext func(context.Context, string, string) (net.Conn, error), probeCount int) map[string]string {
	if probeCount < 1 {
		panic("aTrust node probe count must be positive")
	}
	bestNodes := make(map[string]string)
	for group, nodes := range nodeGroups {
		if len(nodes) > 1 {
			var pingList []ping.TCPing
			var pingNodes []string
			var chList []<-chan struct{}

			for _, node := range nodes {
				host, portText, err := net.SplitHostPort(node)
				if err != nil {
					continue
				}
				port, err := strconv.Atoi(portText)
				if err != nil {
					continue
				}

				tcping := ping.NewTCPing()
				tcping.SetDialContext(dialContext)
				target := ping.Target{
					Protocol: ping.TCP,
					Host:     host,
					Port:     port,
					Counter:  probeCount,
					Interval: time.Duration(0.5 * float64(time.Second)),
					Timeout:  time.Duration(1 * float64(time.Second)),
				}
				tcping.SetTarget(&target)

				pingList = append(pingList, *tcping)
				pingNodes = append(pingNodes, node)
				ch := tcping.Start()
				chList = append(chList, ch)
			}

			for _, ch := range chList {
				<-ch
			}

			bestLatency := int64(0)
			bestNode := ""
			for i, tcping := range pingList {
				result := tcping.Result()
				if result.SuccessCounter == probeCount {
					latency := result.Avg().Milliseconds()

					if bestLatency == 0 || latency < bestLatency {
						bestNode = pingNodes[i]
						bestLatency = latency
					}
				}
			}

			if bestNode != "" {
				bestNodes[group] = bestNode
				log.Printf("Best node in group %s: %s with latency %d ms", group, bestNode, bestLatency)
			} else {
				log.Printf("No reachable node in group %s, using the first node", group)
				bestNodes[group] = nodes[0]
			}
		} else if len(nodes) == 1 {
			bestNodes[group] = nodes[0]
		}
	}

	return bestNodes
}

func (c *Client) updateBestNodes(ctx context.Context, updateBestNodesInterval int) {
	ticker := time.NewTicker(time.Duration(updateBestNodesInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		bestNodes := getBestNodes(c.NodeGroups, c.underlayDialer.DialContext, pingNum)
		c.BestNodesRWMutex.Lock()
		c.BestNodes = bestNodes
		c.BestNodesRWMutex.Unlock()
	}
}
