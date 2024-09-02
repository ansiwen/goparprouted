package lflist

import (
	"sync/atomic"
)

type AssocNode[K comparable, V any] struct {
	next atomic.Pointer[AssocNode[K, V]]
	k    K
	v    atomic.Pointer[V]
}

type Assoc[K comparable, V any] struct {
	head atomic.Pointer[AssocNode[K, V]]
}

func (l *Assoc[K, V]) Store(k K, v *V) (*AssocNode[K, V], bool) {
	return l.store(k, v, false)
}

func (l *Assoc[K, V]) LoadOrStore(k K, v *V) (*AssocNode[K, V], bool) {
	return l.store(k, v, true)
}

// returned bool is true if node existed already
// noop if v == nil
func (l *Assoc[K, V]) store(k K, v *V, loadOrStore bool) (*AssocNode[K, V], bool) {
	if v == nil {
		return nil, false
	}
	var node *AssocNode[K, V]
	nodeRef := &l.head
	for {
		nodeRef, node = findAssocNode(nodeRef, k)
		if node != nil {
			if !loadOrStore {
				node.v.Store(v)
			}
			return node, true
		}
		// at end of list, append new node
		newNode := AssocNode[K, V]{k: k}
		newNode.v.Store(v)
		if nodeRef.CompareAndSwap(nil, &newNode) {
			return &newNode, false
		}
	}
}

func (l *Assoc[K, V]) Load(k K) *AssocNode[K, V] {
	_, node := findAssocNode(&l.head, k)
	return node
}

func (l *Assoc[K, V]) Begin() *AssocNode[K, V] {
	_, node := skipInvalidAssocNodes(&l.head)
	return node
}

func (n *AssocNode[K, V]) Next() *AssocNode[K, V] {
	if n == nil {
		return nil
	}
	_, node := skipInvalidAssocNodes(&n.next)
	return node
}

func (l *Assoc[K, V]) Size() int {
	nodeRef := &l.head
	count := 0
	for {
		_, node := skipInvalidAssocNodes(nodeRef)
		if node == nil {
			return count
		}
		count++
		nodeRef = &node.next
	}
}

func (node *AssocNode[K, V]) Key() K {
	return node.k
}

func (node *AssocNode[K, V]) Val() *V {
	return node.v.Load()
}

func (node *AssocNode[K, V]) ReplaceVal(v *V) bool {
	for {
		old := node.v.Load()
		if old == nil {
			return false
		}
		if node.v.CompareAndSwap(old, v) {
			return true
		}
	}
}

func (node *AssocNode[K, V]) Delete() bool {
	return node.v.Swap(nil) != nil
}

// All is an iterator over the elements of l
// func (l *Assoc[K, V]) All() iter.Seq[*AssocNode[K, V]] {
// 	return func(yield func(*AssocNode[K, V]) bool) {
// 		for n := l.Begin(); n != nil; n = n.Next() {
// 			if !yield(n) {
// 				return
// 			}
// 		}
// 	}
// }

// func (l *Assoc[K, V]) Dump() {
// 	a := make([]*AssocNode[K, V], 0, 1024)
// 	node := l.head.Load()
// 	count := 0
// 	for {
// 		if node == nil {
// 			break
// 		}

// 		a = append(a, node)
// 		count++
// 		node = node.next.Load()
// 	}
// 	for i := range a {
// 		fmt.Printf("node %d: %+v\n", i, a[i])
// 	}
// }

func (n *AssocNode[K, V]) isDeleted() bool {
	return n != nil && n.v.Load() == nil
}

func findAssocNode[K comparable, V any](nodeRef *atomic.Pointer[AssocNode[K, V]], k K) (*atomic.Pointer[AssocNode[K, V]], *AssocNode[K, V]) {
	for {
		var node *AssocNode[K, V]
		nodeRef, node = skipInvalidAssocNodes(nodeRef)
		if node == nil || node.k == k {
			return nodeRef, node
		}
		nodeRef = &node.next
	}
}

func skipInvalidAssocNodes[K comparable, V any](
	nodeRef *atomic.Pointer[AssocNode[K, V]],
) (
	*atomic.Pointer[AssocNode[K, V]],
	*AssocNode[K, V],
) {
	node := nodeRef.Load()
	if node == nil {
		return nodeRef, nil
	}
	// remove deleted nodes with best-effort, not race sensitive
	for node.isDeleted() {
		//nodeRef.Store(&node.next)
		succ := node.next.Load()
		if succ == nil {
			nodeRef = &node.next // never remove last node to allow race-free append, skip instead
			node = nil
			break
		} else {
			nodeRef.Store(succ) // remove deleted node
			node = succ
		}
	}
	return nodeRef, node
}
