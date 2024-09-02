package lflist

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
)

var sizes = []int{
	40,
	100,
	200,
	400,
	800,
	1600,
	10000,
	// 100000,
}

func BenchmarkList(b *testing.B) {
	for _, size := range sizes {
		b.Run(fmt.Sprintf("List-Size-%d", size), func(b *testing.B) {
			var l Simple[bool]
			// var counter atomic.Int64
			x := false
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					l.Add(&x)
					for i := l.Begin(); i != nil; i = i.Next() {
						if rand.Intn(size) == 0 {
							i.Delete()
						}
					}
				}
			})
		})
		b.Run(fmt.Sprintf("Assoc-Size-%d", size), func(b *testing.B) {
			var l Assoc[uint64, bool]
			var id atomic.Uint64
			x := false
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := id.Add(1)
					l.Store(id, &x)
					for i := l.Begin(); i != nil; i = i.Next() {
						if rand.Intn(size) == 0 {
							i.Delete()
						}
					}
				}
			})
		})
		b.Run(fmt.Sprintf("MapMtx-Size-%d", size), func(b *testing.B) {
			m := make(map[uint64]bool)
			var mtx sync.Mutex
			var id atomic.Uint64
			x := false
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := id.Add(1)
					// counter.Add(1)
					mtx.Lock()
					m[id] = x
					for k := range m {
						if rand.Intn(size) == 0 {
							delete(m, k)
						}
					}
					mtx.Unlock()
				}
			})
		})
	}
}

type storage[T any] struct {
	store  func() T
	delete func(T)
}

func addDeleteHelper[T comparable](pb *testing.PB, size int, s storage[T]) {
	var null T
	a := make([]T, size)
	for pb.Next() {
		i := rand.Intn(size)
		if a[i] == null {
			a[i] = s.store()
		} else {
			s.delete(a[i])
			a[i] = null
		}
	}
	// count := 0
	// for i := range a {
	// 	if a[i] == null {
	// 		count++
	// 	}
	// }
	// fmt.Printf("Array population: %d\n", count)
}

func BenchmarkAddDelete(b *testing.B) {
	// b.SetParallelism(10)
	for _, size := range sizes {
		fmt.Println("---------------------------------")
		fmt.Printf("Benchmarks for size %d\n", size)
		b.Run(fmt.Sprintf("Simple-DeleteByPtr-Size-%d", size), func(b *testing.B) {
			var l Simple[struct{}]
			s := storage[*SimpleNode[struct{}]]{
				store: func() *SimpleNode[struct{}] {
					return l.Add(&struct{}{})
				},
				delete: func(e *SimpleNode[struct{}]) {
					e.Delete()
				},
			}
			b.RunParallel(func(pb *testing.PB) {
				addDeleteHelper(pb, size, s)
			})
			// count := 0
			// for i := l.Begin(); i != nil; i = i.Next() {
			// 	count++
			// }
			// fmt.Printf("List size: %d\n", count)
		})
		b.Run(fmt.Sprintf("Simple-DeleteByIdScan-Size-%d", size), func(b *testing.B) {
			var l Simple[uint64]
			var id atomic.Uint64
			s := storage[uint64]{
				store: func() uint64 {
					id := id.Add(1)
					l.Add(&id)
					return id
				},
				delete: func(id uint64) {
					for i := l.Begin(); i != nil; i = i.Next() {
						if v := i.Val(); v != nil && *v == id {
							i.Delete()
						}
					}
				},
			}
			b.RunParallel(func(pb *testing.PB) {
				addDeleteHelper(pb, size, s)
			})
			// count := 0
			// for i := l.Begin(); i != nil; i = i.Next() {
			// 	count++
			// }
			// fmt.Printf("List size: %d\n", count)
		})
		b.Run(fmt.Sprintf("Assoc-DeleteByPtr-Size-%d", size), func(b *testing.B) {
			var l Assoc[uint64, bool]
			var id atomic.Uint64
			true := true
			s := storage[*AssocNode[uint64, bool]]{
				store: func() *AssocNode[uint64, bool] {
					id := id.Add(1)
					n, exists := l.Store(id, &true)
					if exists {
						panic("XXX")
					}
					return n
				},
				delete: func(n *AssocNode[uint64, bool]) {
					n.Delete()
				},
			}
			b.RunParallel(func(pb *testing.PB) {
				addDeleteHelper(pb, size, s)
			})
			// fmt.Printf("Map size: %d\n", len(m))
		})
		b.Run(fmt.Sprintf("Assoc-DeleteById-Size-%d", size), func(b *testing.B) {
			var l Assoc[uint64, bool]
			var id atomic.Uint64
			true := true
			s := storage[uint64]{
				store: func() uint64 {
					id := id.Add(1)
					if _, exists := l.Store(id, &true); exists {
						panic("XXX")
					}
					return id
				},
				delete: func(id uint64) {
					n := l.Load(id)
					if n != nil {
						n.Delete()
					} else {
						panic("XXX")
					}
				},
			}
			b.RunParallel(func(pb *testing.PB) {
				addDeleteHelper(pb, size, s)
			})
			// fmt.Printf("Map size: %d\n", len(m))
		})
		b.Run(fmt.Sprintf("MapMtx-DeleteById-Size-%d", size), func(b *testing.B) {
			m := make(map[uint64]bool)
			var mtx sync.Mutex
			var id atomic.Uint64
			s := storage[uint64]{
				store: func() uint64 {
					id := id.Add(1)
					mtx.Lock()
					m[id] = true
					mtx.Unlock()
					return id
				},
				delete: func(id uint64) {
					mtx.Lock()
					delete(m, id)
					mtx.Unlock()
				},
			}
			b.RunParallel(func(pb *testing.PB) {
				addDeleteHelper(pb, size, s)
			})
			// fmt.Printf("Map size: %d\n", len(m))
		})
		b.Run(fmt.Sprintf("SyncMap-DeleteById-Size-%d", size), func(b *testing.B) {
			var m sync.Map
			var id atomic.Uint64
			s := storage[uint64]{
				store: func() uint64 {
					id := id.Add(1)
					m.Store(id, nil)
					return id
				},
				delete: func(id uint64) {
					m.Delete(id)
				},
			}
			b.RunParallel(func(pb *testing.PB) {
				addDeleteHelper(pb, size, s)
			})
			// count := 0
			// m.Range(func(_, _ any) bool { count++; return true })
			// fmt.Printf("Map size: %d\n", count)
		})
	}
}
