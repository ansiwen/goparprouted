// package notifier is a simple implementation for small amounts of subscriptions. For
// larger sets a two layerd map should be used.

package notifier

import "sync"

type channelMapVal[V comparable] struct {
	ch  chan struct{}
	val V
}

type channelMap[V comparable] map[<-chan struct{}]channelMapVal[V]

type T[V comparable] struct {
	channels channelMap[V]
	mtx      sync.Mutex
}

func (n *T[V]) Add(val V) <-chan struct{} {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	_ = isComparable[V]
	ch := make(chan struct{})
	if n.channels == nil {
		n.channels = make(channelMap[V])
	}
	n.channels[ch] = channelMapVal[V]{ch: ch, val: val}
	return ch
}

func (n *T[V]) Remove(rch <-chan struct{}) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	if v, ok := n.channels[rch]; ok {
		close(v.ch)
	}
	delete(n.channels, rch)
}

func (n *T[V]) Notify(val V) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	for _, v := range n.channels {
		if v.val == val {
			v.ch <- struct{}{}
		}
	}
}

func (n *T[V]) Pending() int {
	return len(n.channels)
}

func isComparable[_ comparable]() {}
