// package notifier is a simple implementation for small amounts of subscriptions. For
// larger sets a two layerd map should be used.

package notifier

import (
	"github.com/ansiwen/goparprouted/internal/lflist"
)

type channelListVal[V comparable] struct {
	ch  chan struct{}
	val V
}

type channelList[V comparable] lflist.Simple[channelListVal[V]]

type Notifier[V comparable] struct {
	channels lflist.Simple[channelListVal[V]]
}

func (n *Notifier[V]) Add(val V) Slot[V] {
	ch := make(chan struct{})
	node := n.channels.Add(&channelListVal[V]{ch: ch, val: val})
	return Slot[V]{node}
}

func (n *Notifier[V]) Notify(val V) {
	for i := n.channels.Begin(); i != nil; i = i.Next() {
		v := i.Val()
		if v.val == val {
			v.ch <- struct{}{}
		}
	}
}

func (n *Notifier[V]) Pending() int {
	count := 0
	for i := n.channels.Begin(); i != nil; i = i.Next() {
		count++
	}
	return count
}

type Slot[V comparable] struct {
	node *lflist.SimpleNode[channelListVal[V]]
}

func (r *Slot[V]) Remove() {
	close(r.node.Val().ch)
	r.node.Delete()
}

func (r *Slot[V]) Ch() <-chan struct{} {
	return r.node.Val().ch
}
