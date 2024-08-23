package netlink

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

func RouteAdd(ip net.IP, iface *net.Interface) error {
	wrapErr := errWrapper("RouteAdd")

	route, err := newHostRoute(ip, iface)
	if err != nil {
		return wrapErr(err)
	}

	if err := netlink.RouteReplace(route); err != nil {
		return wrapErr(fmt.Errorf("netlink.RouteAdd: %w", err))
	}

	return nil
}

func RouteRemove(ip net.IP, iface *net.Interface) error {
	wrapErr := errWrapper("RouteRemove")

	route, err := newHostRoute(ip, iface)
	if err != nil {
		return wrapErr(err)
	}

	if err := netlink.RouteDel(route); err != nil {
		return wrapErr(fmt.Errorf("netlink.RouteDel: %w", err))
	}

	return nil
}

func UpdateKernelARPTable(ip net.IP, mac net.HardwareAddr, iface *net.Interface, perm bool) error {
	link, err := netlink.LinkByIndex(iface.Index)
	if err != nil {
		return fmt.Errorf("UpdateKernelARPTable: netlink.LinkByIndex: %w", err)
	}
	neighbor := &netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		State:        netlink.NUD_REACHABLE,
		IP:           ip,
		HardwareAddr: mac,
	}
	if perm {
		neighbor.State = netlink.NUD_PERMANENT
	}
	return netlink.NeighSet(neighbor)
}

func RemoveFromKernelARPTable(ip net.IP, iface *net.Interface) error {
	link, err := netlink.LinkByIndex(iface.Index)
	if err != nil {
		return fmt.Errorf("RemoveFromKernelARPTable: netlink.LinkByIndex: %w", err)
	}
	neighbor := &netlink.Neigh{
		LinkIndex: link.Attrs().Index,
		IP:        ip,
	}
	return netlink.NeighDel(neighbor)
}

func newHostRoute(ip net.IP, iface *net.Interface) (*netlink.Route, error) {
	link, err := netlink.LinkByIndex(iface.Index)
	if err != nil {
		return nil, fmt.Errorf("newHostRoute: netlink.LinkByIndex: %w", err)
	}

	dst := &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(32, 32), // /32 for single IP address
	}

	return &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Scope:     netlink.SCOPE_LINK,
		Priority:  0,
	}, nil
}

func errWrapper(msg string) func(err error) error {
	format := msg + ": %w"
	return func(err error) error {
		return fmt.Errorf(format, err)
	}
}
