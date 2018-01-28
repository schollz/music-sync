package ssh

import (
	"encoding/json"
	"fmt"
	"github.com/LogicalOverflow/music-sync/logging"
	"github.com/LogicalOverflow/music-sync/util"
	"github.com/chzyer/readline"
	"github.com/gliderlabs/ssh"
	"io"
	"os"
	"strings"
)

var HostKeyFile string

var logger = log.GetLogger("ssh")

func ReadUsersFile(filename string) (map[string]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open users file (%s): %v", filename, err)
	}
	users := make(map[string]string)
	if err := json.NewDecoder(f).Decode(&users); err != nil {
		return nil, fmt.Errorf("failed to decode users file (%s): %v", filename, err)
	}
	return users, nil
}

func StartSSH(address string, users map[string]string) {
	ssh.Handle(func(s ssh.Session) {
		defer s.Close()

		logger.Infof("new ssh connection from %s as %s", s.RemoteAddr(), s.User())
		pty, resize, ok := s.Pty()
		if !ok {

			logger.Infof("no pty req from %s as %s", s.RemoteAddr(), s.User())
			return
		}
		width, height := pty.Window.Width, pty.Window.Height
		go func() {
			for w := range resize {
				width, height = w.Width, w.Height
			}
		}()

		tcfg := &readline.Config{
			Prompt: "\033[31mmusic-sync»\033[0m ",

			AutoComplete: &sshAutoCompleter{},

			VimMode: false,

			InterruptPrompt: "^C",
			EOFPrompt:       "exit",

			FuncGetWidth: func() int { return width },

			Stdin:       s,
			Stdout:      s,
			StdinWriter: s,
			Stderr:      s.Stderr(),

			UniqueEditLine: false,
		}
		ex, err := readline.NewEx(tcfg)
		if err != nil {
			logger.Warnf("failed to create ex: %v", err)
			fmt.Fprintf(s, "failed to create ex: %v\n", err)
			return
		}
		defer ex.Close()
		ex.Clean()
		for {
			line, err := ex.Readline()
			if err == readline.ErrInterrupt {
			} else if err == io.EOF {
				logger.Infof("connection %s as %s closed", s.RemoteAddr(), s.User())
				return
			} else if err != nil {
				logger.Infof("connection error from %s as %s: %v", s.RemoteAddr(), s.User(), err)
				return
			}

			parts := make([]string, 1)
			parts[0] = strings.TrimSpace(line)
			for strings.Contains(parts[len(parts)-1], " ") {
				l := parts[len(parts)-1]
				i := strings.Index(l, " ")
				parts[len(parts)-1] = strings.TrimSpace(l[:i])
				parts = append(parts, strings.TrimSpace(l[i:]))
			}
			cmd, args := parts[0], parts[1:]

			known := false
			for _, c := range commands {
				if c.Name == cmd {
					s, ok := c.Exec(args)
					if ok {
						if strings.HasSuffix(s, "\n") {
							s = s[:len(s)-1]
						}
						fmt.Fprintln(ex, s)
					} else {
						fmt.Fprintf(ex, "%s\n", c.usage())
					}
					known = true
					break
				}
			}
			if !known {
				switch cmd {
				case "clear":
					fmt.Fprint(ex, "\033[H")
				case "exit":
					logger.Infof("connection %s as %s closed", s.RemoteAddr(), s.User())
					return
				default:
					fmt.Fprintf(ex, "Unknown command '%s'. Type 'help' for help.\n", cmd)
				}
			}
		}

	})

	logger.Infof("starting ssh server at %s", address)
	options := make([]ssh.Option, 0)
	if HostKeyFile == "" {
		logger.Warnf("no host key file provided, generating a new host key")
	} else if err := util.CheckFile(HostKeyFile); err != nil {
		logger.Warnf("unable to access host key file, generating new host key: %v", err)
	} else {
		options = append(options, ssh.HostKeyFile(HostKeyFile))
	}
	options = append(options, ssh.PasswordAuth(func(ctx ssh.Context, password string) bool {
		expected, ok := users[ctx.User()]
		if ok && expected == password {
			return true
		} else {
			logger.Warnf("failed ssh login attempt from %s as %s", ctx.RemoteAddr(), ctx.User())
			return false
		}
	}))

	// TODO: add public key auth support
	err := ssh.ListenAndServe(address, nil, ssh.PasswordAuth(func(ctx ssh.Context, password string) bool {
		expected, ok := users[ctx.User()]
		logger.Infof("ssh login attempt by %s", ctx.User())
		return ok && expected == password
	}))
	logger.Errorf("ssh server at %s stopped: %v", address, err)
}

type sshAutoCompleter struct{}

func (sac *sshAutoCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	newLine = make([][]rune, 0)

	for _, c := range commands {
		if string(line) == c.Name {
			if c.Options != nil {
				for _, o := range c.Options("", 0) {
					newLine = append(newLine, []rune(c.Name + " " + o)[pos:])
				}
			}
		} else if strings.HasPrefix(string(line), c.Name+" ") {
			if c.Options != nil {
				argStart := strings.LastIndex(string(line), " ") + 1
				baseLine := string(line)[:argStart]
				prefix := string(line)[argStart:]
				argNum := -1
				for i := range line {
					if i != 0 && line[i-1] != ' ' && line[i] == ' ' {
						argNum++
					}
				}
				for _, o := range c.Options(prefix, argNum) {
					newLine = append(newLine, []rune(baseLine + o)[pos:])
				}
			}
		}
	}

	for _, c := range commands {
		if strings.HasPrefix(c.Name, string(line)) {
			newLine = append(newLine, []rune(c.Name + " ")[pos:])
		}
	}

	for _, c := range []string{"clear", "exit"} {
		if strings.HasPrefix(c, string(line)) {
			newLine = append(newLine, []rune(c)[pos:])
		}
	}
	length = pos
	return
}