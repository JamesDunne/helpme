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

func forward(local, remote net.Conn, logContext string) {
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
	log.Printf("%s: closed\n", logContext)
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

	func() {
		// Connect to SSH server:
		log.Printf("Connecting to SSH server %s...\n", sshAddr)
		conn, err := ssh.Dial("tcp", sshAddr, sshConfig)
		if err != nil {
			log.Printf("unable to connect: %s\n", err)
			return
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

		if *isHelper {
			// Helping someone else.

			// Set up forwarding addresses:
			localAddr := net.JoinHostPort("0.0.0.0", *localPort)
			remoteAddr := net.JoinHostPort("127.0.0.1", *remotePort)

			log.Printf("Forwarding local connections from %s to remote %s\n", localAddr, remoteAddr)

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
					if err != nil {
						log.Printf("Accept: %s\n", err)
						done <- true
						break
					}
					logContext := local.RemoteAddr().String()
					log.Printf("%s: Accepted local connection.\n", logContext)

					// Connect to the remote RDP service:
					remote, err := conn.Dial("tcp", remoteAddr)
					if err != nil {
						log.Printf("%s: Unable to connect via SSH: %s\n", logContext, err)
						local.Close()
						continue
					}
					defer remote.Close()

					// Start forwarding data back 'n forth:
					go forward(local, remote, logContext)
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

			log.Printf("Forwarding remote connections from %s to local %s\n", remoteAddr, localAddr)

			go func() {
				// Request the remote side to open a port for forwarding:
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
					if err != nil {
						log.Printf("Accept: %s\n", err)
						done <- true
						break
					}
					logContext := remote.RemoteAddr().String()
					log.Printf("%s: Accepted new SSH tunnel connection.\n", logContext)

					// Connect to the local RDP service:
					local, err := net.DialTCP("tcp", nil, localTCPAddr)
					if err != nil {
						log.Printf("%s: Could not open local connection to: %s\n", logContext, localTCPAddr)
						remote.Close()
						continue
					}

					// Start forwarding data back 'n forth:
					go forward(local, remote, logContext)
				}
			}()
		}

		<-done
	}()

	log.Printf("Done. Press any key.\n")
	var buffer [1]byte
	_, _ = os.Stdin.Read(buffer[:])
}
