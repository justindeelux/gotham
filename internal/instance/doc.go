// Package instance holds the operator-editable settings of the Gotham
// instance itself: general values (control-plane URL, instance name,
// timezone), host network configuration (DNS, IPv4, IPv6) and Linux system
// options (hostname, NTP).
//
// General values resolve with the precedence env > database > built-in
// default; a field set through its environment variable is reported as locked
// and cannot be edited. Network and system values are the desired host state:
// they are applied through a fixed, root-owned helper (see HostApplier and
// deploy/gotham-hostctl.sh) that is reached via sudo with a closed verb set and
// re-validates every value. Network changes are applied tentatively, must be
// confirmed within a deadline and are reverted by the host when they are not,
// so a wrong address can never lock the operator out.
package instance
