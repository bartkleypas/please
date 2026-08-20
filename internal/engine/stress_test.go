package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestGraph_ConcurrencyStress(t *testing.T) {
	g := NewGraph()
	now := time.Now()

	// Initial root
	root := &Node{ID: "root", ParentID: "", Role: RoleSystem, Content: "Root", Timestamp: now}
	g.AddNode(root)

	var writerWg sync.WaitGroup
	var readerWg sync.WaitGroup
	numWriters := 20
	numReaders := 20
	nodesPerWriter := 100

	stopReaders := make(chan bool)

	// 1. Concurrent Writers
	for i := 0; i < numWriters; i++ {
		writerWg.Add(1)
		go func(writerID int) {
			defer writerWg.Done()
			for j := 0; j < nodesPerWriter; j++ {
				nodeID := fmt.Sprintf("W%d-N%d", writerID, j)
				parentID := "root"
				if j > 0 {
					parentID = fmt.Sprintf("W%d-N%d", writerID, j-1)
				}

				node := &Node{
					ID:        nodeID,
					ParentID:  parentID,
					Role:      RoleAssistant,
					Content:   "Stress Content",
					Timestamp: time.Now(),
				}
				g.AddNode(node)
				time.Sleep(time.Microsecond * 10)
			}
		}(i)
	}

	// 2. Concurrent Readers
	for i := 0; i < numReaders; i++ {
		readerWg.Add(1)
		go func(readerID int) {
			defer readerWg.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
					for w := 0; w < numWriters; w++ {
						targetNodeID := fmt.Sprintf("W%d-N%d", w, nodesPerWriter/2)
						_, _ = g.GetPath(targetNodeID)
						_ = g.GetChildren("root")
						_ = g.GetRoots()
					}
					time.Sleep(time.Microsecond * 5)
				}
			}
		}(i)
	}

	// Wait for writers to finish
	writerWg.Wait()

	// Shutdown readers
	close(stopReaders)
	readerWg.Wait()
}
