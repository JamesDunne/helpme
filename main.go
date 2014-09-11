package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

import "code.google.com/p/go.crypto/ssh"

var localTCPAddr *net.TCPAddr
var done chan bool

func forward(local, remote net.Conn) {
	copyComplete := make(chan bool, 1)

	// Copy data back 'n forth:
	go func() {
		defer remote.Close()
		io.Copy(remote, local)
		//log.Printf("%s: Done copying local to remote.\n", remote.RemoteAddr())
		copyComplete <- true
	}()

	go func() {
		defer local.Close()
		io.Copy(local, remote)
		//log.Printf("%s: Done copying remote to local.\n", remote.RemoteAddr())
		copyComplete <- true
	}()

	<-copyComplete
	log.Printf("%s: closed\n", remote.RemoteAddr())
}

func main() {
	// Define commandline flags:
	isHelper := flag.Bool("helper", false, "Use this to connect to someone asking for help")
	sshServer := flag.String("ssh", "", "SSH server:port to connect to")
	sshUserName := flag.String("user", "", "SSH username")
	sshPassword := flag.String("pass", "", "SSH password")
	localPort := flag.String("lport", "3389", "Local port")
	remotePort := flag.String("rport", "3389", "Remote port")
	flag.Parse()

	// SSH connection details:
	sshAddr := *sshServer
	sshConfig := &ssh.ClientConfig{
		User: *sshUserName,
		Auth: []ssh.AuthMethod{
			ssh.Password(*sshPassword),
		},
	}

	// Connect to SSH server:
	log.Printf("Connecting to SSH server %s...\n", sshAddr)
	conn, err := ssh.Dial("tcp", sshAddr, sshConfig)
	if err != nil {
		log.Fatalf("unable to connect: %s", err)
	}
	log.Printf("Connected.\n")
	defer conn.Close()

	done := make(chan bool, 1)

	// Intercept termination signals:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<-sigc
		done <- true
	}()

	func() {
		if *isHelper {
			// Helping someone else.

			// Set up forwarding addresses:
			localAddr := net.JoinHostPort("0.0.0.0", *localPort)
			remoteAddr := net.JoinHostPort("127.0.0.1", *remotePort)

			go func() {
				localListener, err := net.Listen("tcp", localAddr)
				if err != nil {
					log.Printf("unable to listen: %s\n", err)
					done <- true
					return
				}
				defer localListener.Close()
				log.Printf("Listening...\n")

				// Begin accepting new connections from the SSH tunnel:
				for {
					// Accept a new local connection:
					local, err := localListener.Accept()
					log.Printf("%s: Accepted local connection.\n", local.RemoteAddr())
					if err != nil {
						log.Printf("Accept: %s\n", err)
						done <- true
						break
					}

					// Connect to the remote RDP service:
					remote, err := conn.Dial("tcp", remoteAddr)
					if err != nil {
						log.Printf("%s: Unable to connect via SSH: %s\n", local.RemoteAddr(), err)
						done <- true
						return
					}
					defer remote.Close()

					// Start forwarding data back 'n forth:
					go forward(local, remote)
				}
			}()
		} else {
			// Being helped.

			// Set up forwarding addresses:
			remoteAddr := net.JoinHostPort("0.0.0.0", *remotePort)
			localAddr := net.JoinHostPort("127.0.0.1", *localPort)

			// Resolve local address:
			var err error
			localTCPAddr, err = net.ResolveTCPAddr("tcp", localAddr)
			if err != nil {
				log.Printf("unable to resolve: %s\n", err)
				done <- true
				return
			}

			go func() {
				// Request the remote side to open a port for forwarding:
				log.Printf("Listen for remote connections on %s...\n", remoteAddr)
				l, err := conn.Listen("tcp", remoteAddr)
				if err != nil {
					log.Printf("unable to register tcp forward: %s\n", err)
					done <- true
					return
				}
				log.Printf("Listening...\n")
				defer l.Close()

				// Begin accepting new connections from the SSH tunnel:
				for {
					// Accept a new remote connection from SSH:
					remote, err := l.Accept()
					log.Printf("%s: Accepted new SSH tunnel connection.\n", remote.RemoteAddr())
					if err != nil {
						log.Printf("Accept: %s\n", err)
						done <- true
						break
					}

					// Connect to the local RDP service:
					local, err := net.DialTCP("tcp", nil, localTCPAddr)
					if err != nil {
						log.Printf("%s: Could not open local connection to: %s\n", remote.RemoteAddr(), localTCPAddr)
						remote.Close()
						done <- true
						return
					}

					// Start forwarding data back 'n forth:
					go forward(local, remote)
				}
			}()
		}
	}()

	<-done
	log.Printf("Done.\n")
}
