//go:build integration

package db

// AdmitReplicas puts every replica in the rotation as a health pass that found
// it fit would, so a test can judge a read between two passes.
func (c *Cluster) AdmitReplicas() {
	for _, r := range c.replicas {
		r.healthy.Store(true)
	}
}
