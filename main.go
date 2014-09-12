package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

import "code.google.com/p/go.crypto/ssh"

// Forward TCP traffic back 'n forth between local and remote connections:
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

// Start port forwarding for the given SSH client:
func loop(sshClient *ssh.Client, localToRemote, remoteToLocal []PortForward) {
	done := make(chan bool, 1)

	// Intercept termination signals:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		sig := <-sigc
		log.Printf("Caught signal: %s\n", sig)
		done <- true
	}()

	// Forward all the local-to-remote ports:
	for _, fwd := range localToRemote {
		// Set up forwarding addresses:
		localAddr := fwd.LocalAddr
		remoteAddr := fwd.RemoteAddr

		log.Printf("Forwarding connections from local %s to remote %s\n", localAddr, remoteAddr)

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
					break
				}
				logContext := local.RemoteAddr().String()
				log.Printf("%s: Accepted local connection.\n", logContext)

				// Connect to the remote RDP service:
				remote, err := sshClient.Dial("tcp", remoteAddr)
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
	}

	// Forward all remote-to-local ports:
	for _, fwd := range remoteToLocal {
		// Set up forwarding addresses:
		localAddr := fwd.LocalAddr
		remoteAddr := fwd.RemoteAddr

		// Resolve local address:
		var err error
		localTCPAddr, err := net.ResolveTCPAddr("tcp", localAddr)
		if err != nil {
			log.Printf("unable to resolve local address: %s\n", err)
			done <- true
			return
		}

		log.Printf("Forwarding connections from remote %s to local %s\n", remoteAddr, localAddr)

		go func() {
			// Request the remote side to open a port for forwarding:
			l, err := sshClient.Listen("tcp", remoteAddr)
			if err != nil {
				log.Printf("unable to register tcp forward: %s\n", err)
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
}

func main() {
	// Define commandline flags:
	profileName := flag.String("profile", "rdp_server", "Port-forwarding profile name to use (e.g. 'rdp_server', 'rdp_client', etc.); default is 'rdp_server'")
	sshHostFlag := flag.String("host", "", "SSH host to connect to")
	sshUserFlag := flag.String("user", "", "SSH user")
	sshPasswordFlag := flag.String("password", "", "SSH password")
	nopromptForCredentials := flag.Bool("noprompt", false, "Do not prompt for SSH connection details and credentials")
	nokeyExit := flag.Bool("nokeyexit", false, "Do not wait for a keypress to exit")
	flag.Parse()

	// TODO: Parse extra profiles from JSON configuration

	profile, ok := DefaultProfiles[*profileName]
	if !ok {
		log.Printf("Unable to find profile named '%s'\n", profileName)
		return
	}

	// Create a prompt() function to scan stdin:
	stdinLines := bufio.NewScanner(os.Stdin)
	var prompt = func(prompt string) string {
		fmt.Print(prompt)
		for !stdinLines.Scan() {
		}
		return stdinLines.Text()
	}

	// SSH connection details:
	sshAddr := profile.SSH.Host
	if sshAddr == "" {
		sshAddr = *sshHostFlag
		if sshAddr == "" {
			if *nopromptForCredentials {
				log.Printf("-host argument is required to specify SSH host to connect to\n")
				return
			} else {
				sshAddr = prompt("SSH host:     ")
			}
		}
	}

	// If port is missing, set it to 22:
	{
		_, _, err := net.SplitHostPort(sshAddr)
		if err != nil {
			if addrError, ok := err.(*net.AddrError); ok {
				if addrError.Err == "missing port in address" {
					sshAddr = sshAddr + ":22"
				}
			} else {
				log.Printf("Could not parse SSH host.\n")
				return
			}
		}
	}

	sshConfig := &ssh.ClientConfig{
		User: profile.SSH.User,
		Auth: []ssh.AuthMethod{},
	}

	if sshConfig.User == "" {
		sshConfig.User = *sshUserFlag
		if sshConfig.User == "" {
			if *nopromptForCredentials {
				log.Printf("-user argument is required to specify SSH user to log in as\n")
				return
			} else {
				sshConfig.User = prompt("SSH user:     ")
			}
		}
	}

	// Populate authentication methods:
	for _, auth := range profile.SSH.Auth {
		switch auth.Kind {
		case SSHAuthPassword:
			// Override blank password data with commandline argument:
			password := auth.Data
			if password == "" {
				password = *sshPasswordFlag
				if password == "" {
					if *nopromptForCredentials {
						log.Printf("-password argument is required to specify SSH user's password\n")
						return
					} else {
						password = prompt("SSH password: ")
					}
				}
			}
			sshConfig.Auth = append(sshConfig.Auth, ssh.Password(password))
		case SSHAuthPublicKey:
			// TODO!
			panic(nil)
		}
	}

	// Connect to SSH server:
	log.Printf("Connecting to SSH server '%s'...\n", sshAddr)
	sshClient, err := ssh.Dial("tcp", sshAddr, sshConfig)
	if err != nil {
		log.Printf("Unable to connect to SSH server: %s\n", err)
		return
	}
	log.Printf("Connected.\n")
	defer sshClient.Close()

	loop(sshClient, profile.LocalToRemote, profile.RemoteToLocal)

	if !*nokeyExit {
		// Horrendous hack to do a simple getch(). Terminal will usually be in cooked mode so will just read extra crap.
		log.Printf("Press enter to exit.")
		var buffer [1]byte
		_, _ = os.Stdin.Read(buffer[:])
	}
}
