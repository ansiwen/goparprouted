package arp

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/ansiwen/goparprouted/internal/lflist"
	"github.com/ansiwen/goparprouted/internal/netlink"
	"github.com/ansiwen/goparprouted/internal/notifier"

	"github.com/mdlayher/arp"
	"github.com/mdlayher/ethernet"
)

const (
	arpRequestTimeout       = 5 * time.Second
	arpTableSweepInterval   = 10 * time.Second
	arpTableEntryTimeout    = 120 * time.Second
	arpTableRefreshInterval = 50 * time.Second
)

var getID = func() func() uint64 {
	var id atomic.Uint64
	return func() uint64 {
		return id.Add(1)
	}
}()

type arpTableEntry struct {
	IPAddr    netip.Addr
	HWAddr    net.HardwareAddr
	Interface *net.Interface
	Timestamp time.Time
}

type ARPProxy struct {
	arpTable     lflist.Assoc[netip.Addr, arpTableEntry]
	arpListeners []*listener
	notifier     notifier.Notifier[netip.Addr]
	debug        bool
	defaultRoute bool
	arpPerm      bool
}

type listener struct {
	*ARPProxy
	iface   *net.Interface
	client  *arp.Client
	current pktRegistry
}

type context struct {
	*listener
	id  uint64
	pkt *arp.Packet
}

func NewARPProxy() *ARPProxy {
	var ap ARPProxy
	return &ap
}

func (ap *ARPProxy) AddInterface(iface *net.Interface) {
	client, err := arp.Dial(iface)
	if err != nil {
		log.Fatalf("Failed to create ARP client for interface %s: %v", iface.Name, err)
	}
	listener := listener{ARPProxy: ap, iface: iface, client: client}
	ap.arpListeners = append(ap.arpListeners, &listener)
}

func (ap *ARPProxy) SetDebug(b bool) {
	ap.debug = b
}

func (ap *ARPProxy) SetDefaultRt(b bool) {
	ap.defaultRoute = b
}

func (ap *ARPProxy) SetARPPerm(b bool) {
	ap.arpPerm = b
}

func (ap *ARPProxy) Start() {
	for _, listener := range ap.arpListeners {
		go listener.start()
	}
	if !ap.arpPerm {
		go func() {
			for {
				time.Sleep(arpTableSweepInterval)
				ap.sweepARPTable(false)
			}
		}()
	}
}

func (ap *ARPProxy) Cleanup() {
	ap.sweepARPTable(true)
}

func (ap *ARPProxy) sweepARPTable(cleanup bool) {
	refreshEntry := func(entry *arpTableEntry) {
		var client *arp.Client
		for _, l := range ap.arpListeners {
			if l.iface == entry.Interface {
				client = l.client
				break
			}
		}

		if client == nil {
			log.Printf("No client for interface %s found.", entry.Interface.Name)
			return
		}

		req, err := arp.NewPacket(arp.OperationRequest,
			entry.Interface.HardwareAddr, netip.IPv4Unspecified(),
			entry.HWAddr, entry.IPAddr,
		)
		if err != nil {
			log.Printf("Failed to create refresh request: %v", err)
			return
		}
		ap.trace("Sending refresh ARP request to interface %s: %s", entry.Interface.Name, pktStr(req))
		if err := client.WriteTo(req, req.TargetHardwareAddr); err != nil {
			log.Printf("Failed to send refresh request: %v", err)
		}
	}

	removeEntry := func(n *lflist.AssocNode[netip.Addr, arpTableEntry]) {
		entry := n.Val()
		ap.trace("Removing expired entry for %s", entry.IPAddr)
		n.Delete()
		if entry.isNegative() {
			return
		}
		if !ap.hasDefaultRoute(entry.Interface) {
			ap.trace("Removing route for %s to %s", entry.IPAddr, entry.Interface.Name)
			err := netlink.RouteRemove(entry.IPAddr.AsSlice(), entry.Interface)
			if err != nil {
				log.Printf("Removing host route failed: %v", err)
			}
		}
		netlink.RemoveFromKernelARPTable(entry.IPAddr.AsSlice(), entry.Interface)
	}

	ap.trace("Sweeping ARP table entries")

	for n := ap.arpTable.Begin(); n != nil; n = n.Next() {
		entry := n.Val()
		age := time.Since(entry.Timestamp)
		if !cleanup && !entry.isNegative() && age > arpTableRefreshInterval {
			refreshEntry(entry)
		}
		if cleanup || age > arpTableEntryTimeout {
			removeEntry(n)
		}
	}
}

func (ap *ARPProxy) hasDefaultRoute(iface *net.Interface) bool {
	return ap.defaultRoute && iface == ap.arpListeners[0].iface
}

func (e *arpTableEntry) isNegative() bool {
	return e.HWAddr == nil
}

func (l *listener) start() {
	defer l.client.Close()
	for {
		pkt, _, err := l.client.Read()
		if err != nil {
			log.Printf("Error reading ARP packet on interface %s: %v", l.iface.Name, err)
			continue
		}
		reg := l.current.register(pkt)
		if reg == nil {
			l.trace("Skipping duplicate packet on %s: %s (reg size: %d)", l.iface.Name, pktStr(pkt), l.current.pkts.Size())
			continue
		}
		ctx := context{
			listener: l,
			id:       getID(),
			pkt:      pkt,
		}
		ctx.trace("Received on %s: %s", l.iface.Name, pktStr(pkt))
		go func() {
			switch pkt.Operation {
			case arp.OperationRequest:
				ctx.handleARPRequest()
			case arp.OperationReply:
				ctx.handleARPReply()
			default:
				log.Printf("Invalid ARP packet: %s", pktStr(pkt))
			}
			unregister(reg)
			ctx.trace("Finished")
		}()
	}
}

func (ctx *context) handleARPRequest() {
	req := ctx.pkt
	logErr := errLogger("Failed to handle ARP Request")

	handleWithARPTable := func() bool {
		node := ctx.arpTable.Load(req.TargetIP)
		if node == nil {
			return false
		}
		entry := node.Val()
		if entry == nil {
			return false
		}
		if !entry.isNegative() {
			if entry.Interface != ctx.iface {
				ctx.trace("Sending ARP proxy reply to %s on interface %s", ctx.pkt.SenderIP, ctx.iface.Name)
				if err := ctx.client.Reply(ctx.pkt, ctx.iface.HardwareAddr, ctx.pkt.TargetIP); err != nil {
					log.Printf("sendReply: arp.Client.Reply: %v", err)
				}
			} else {
				ctx.trace("Found entry from same interface, ignoring request for %s from %s on interface %s", ctx.pkt.TargetIP, ctx.pkt.SenderIP, ctx.iface.Name)
			}
			return true
		}
		// negative entry
		if entry.Interface == ctx.iface {
			ctx.trace("Found matching negative entry, ignoring request for %s from %s on interface %s", ctx.pkt.TargetIP, ctx.pkt.SenderIP, ctx.iface.Name)
			return true
		}
		return false
	}

	if handleWithARPTable() {
		return
	}

	// Relay ARP request to other interfaces
	for _, otherListener := range ctx.arpListeners {
		otherIface := otherListener.iface
		if otherIface != ctx.iface {
			fwdReq, err := arp.NewPacket(arp.OperationRequest,
				otherIface.HardwareAddr, req.SenderIP,
				make(net.HardwareAddr, 6), req.TargetIP,
			)
			if err != nil {
				logErr(fmt.Errorf("creating forwarded request: %w", err))
				continue
			}
			ctx.trace("Forwarding ARP request to interface %s: %s", otherIface.Name, pktStr(fwdReq))
			if err := otherListener.client.WriteTo(fwdReq, ethernet.Broadcast); err != nil {
				logErr(err)
			}
		}
	}

	if ctx.waitFor(req.TargetIP, arpRequestTimeout) {
		if !handleWithARPTable() {
			logErr(fmt.Errorf("no entry for %s despite notification", req.TargetIP))
		}
	} else {
		ctx.trace("ARP Request timed out, creating negative entry for %s on %s", req.TargetIP, ctx.iface.Name)
		// make sure we never overwrite another entry
		entry := &arpTableEntry{
			IPAddr:    req.TargetIP,
			HWAddr:    nil,
			Interface: ctx.iface, // in negative entries this is the interface the request was _coming_ from
			Timestamp: time.Now(),
		}
		_, existed := ctx.arpTable.LoadOrStore(req.TargetIP, entry)
		if existed {
			ctx.trace("Creating negative entry failed, another entry exists.")
		}
	}
}

func (ctx *context) handleARPReply() {
	reply := ctx.pkt
	logErr := errLogger("Failed to handle ARP Reply")

	if reply.SenderIP == netip.IPv4Unspecified() {
		return
	}

	newEntry := true
	existingNode := ctx.arpTable.Load(reply.SenderIP)
	if existingNode != nil {
		entry := existingNode.Val()
		if entry != nil && !entry.isNegative() && !(entry.Interface != ctx.iface) {
			newEntry = false
		}
	}

	if newEntry {
		ctx.trace("Creating new ARP entry for %s to %s on %s", reply.SenderIP, reply.SenderHardwareAddr, ctx.iface.Name)
		defer ctx.notifier.Notify(reply.SenderIP)
	} else {
		ctx.trace("Updateing ARP entry for %s to %s on %s", reply.SenderIP, reply.SenderHardwareAddr, ctx.iface.Name)
	}

	entry := &arpTableEntry{
		IPAddr:    reply.SenderIP,
		HWAddr:    reply.SenderHardwareAddr,
		Interface: ctx.iface,
		Timestamp: time.Now(),
	}
	if existingNode == nil || !existingNode.ReplaceVal(entry) {
		ctx.arpTable.Store(entry.IPAddr, entry)
	}

	// Update kernel ARP table
	if err := netlink.UpdateKernelARPTable(reply.SenderIP.AsSlice(), reply.SenderHardwareAddr, ctx.iface, ctx.arpPerm); err != nil {
		logErr(err)
	}

	if newEntry {
		if !ctx.hasDefaultRoute(ctx.iface) {
			ctx.trace("Adding route for %s to %s", reply.SenderIP, ctx.iface.Name)
			if err := netlink.RouteAdd(reply.SenderIP.AsSlice(), ctx.iface); err != nil {
				logErr(err)
			}
		} else {
			ctx.trace("Not adding route for %s to %s, covered by default route", reply.SenderIP, ctx.iface.Name)
		}
	}
}

func (ctx *context) waitFor(ip netip.Addr, timeout time.Duration) bool {
	slot := ctx.notifier.Add(ip)
	ctx.trace("Waiting for %s entry (notifylist size: %d)", ip, ctx.notifier.Pending())
	defer slot.Remove()
	select {
	case <-slot.Ch():
		ctx.trace("Received notification for %s", ip)
		return true
	case <-time.After(timeout):
		ctx.trace("Waiting for %s timed out", ip)
		return false
	}
}

func errLogger(msg string) func(err error) {
	format := msg + ": %v"
	return func(err error) {
		log.Printf(format, err)
	}
}

func pktStr(pkt *arp.Packet) string {
	if pkt.Operation == arp.OperationRequest {
		return fmt.Sprintf("ARP Request: who-has %s (%s), tell %s (%s)", pkt.TargetIP, pkt.TargetHardwareAddr, pkt.SenderIP, pkt.SenderHardwareAddr)
	} else {
		return fmt.Sprintf("ARP Reply: %s is-at %s (sent to %s (%s))", pkt.SenderIP, pkt.SenderHardwareAddr, pkt.TargetIP, pkt.TargetHardwareAddr)
	}
}

func (ap *ARPProxy) trace(format string, v ...any) {
	if ap.debug {
		log.Printf(format, v...)
	}
}

func (ctx *context) trace(format string, v ...any) {
	if ctx.debug {
		format = fmt.Sprintf("[%d] ", ctx.id) + format
		log.Printf(format, v...)
	}
}

type pktRegistry struct {
	pkts lflist.Assoc[string, struct{}]
}

func (v *pktRegistry) register(pkt *arp.Packet) *lflist.AssocNode[string, struct{}] {
	data, _ := pkt.MarshalBinary()
	dataStr := string(data)
	node, exists := v.pkts.Store(dataStr, &struct{}{})
	if exists {
		return nil
	}
	return node
}

func unregister(n *lflist.AssocNode[string, struct{}]) {
	n.Delete()
}
