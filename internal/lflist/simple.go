package lflist

import (
	"sync/atomic"
)

type SimpleNode[T any] struct {
	next atomic.Pointer[SimpleNode[T]]
	p    atomic.Pointer[T]
}

type Simple[T any] struct {
	head atomic.Pointer[SimpleNode[T]]
}

func (l *Simple[T]) Add(v *T) *SimpleNode[T] {
	var newNode SimpleNode[T]
	newNode.p.Store(v)
	for {
		h := l.head.Load()
		next := skipInvalidSimpleNodes(h)
		newNode.next.Store(next)
		if l.head.CompareAndSwap(h, &newNode) {
			return &newNode
		}
	}
}

func (l *Simple[T]) Begin() *SimpleNode[T] {
	return skipInvalidSimpleNodes(l.head.Load())
}

func (n *SimpleNode[T]) Next() *SimpleNode[T] {
	return nextValidSimpleNode(n)
}

func (n *SimpleNode[T]) isDeleted() bool {
	return n != nil && n.p.Load() == nil
}

func skipInvalidSimpleNodes[T any](node *SimpleNode[T]) *SimpleNode[T] {
	if node.isDeleted() {
		node = nextValidSimpleNode(node)
	}
	return node
}

func nextValidSimpleNode[T any](node *SimpleNode[T]) *SimpleNode[T] {
	if node == nil {
		return nil
	}
	succ := node.next.Load()
	// remove deleted nodes with best-effort
	// not race sensitive, because insert happens only at the head
	for succ.isDeleted() {
		succ = succ.next.Load()
		node.next.Store(succ)
	}
	return succ
}

func (n *SimpleNode[T]) Delete() bool {
	if n.p.Swap(nil) == nil {
		return false
	}
	return true
}

func (n *SimpleNode[T]) Val() *T {
	return n.p.Load()
}
