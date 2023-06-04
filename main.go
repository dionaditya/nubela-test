package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/oleiade/lane"
)

type Request struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

type Response struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result"`
}

func main() {
	socketPath := "/var/run/dev-test/sock"

	// Handle termination signals to clean up the socket file
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		cleanupSocket(socketPath)
		os.Exit(0)
	}()

	// Create the UNIX domain socket
	err := createSocket(socketPath)
	if err != nil {
		log.Fatal("Failed to create UNIX domain socket:", err)
	}

	log.Println("Server started. Listening on", socketPath)

	// Start accepting connections
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatal("Failed to listen on UNIX domain socket:", err)
	}
	defer func() {
		listener.Close()
		cleanupSocket(socketPath)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept connection:", err)
			continue
		}

		go handleConnection(conn)
	}
}

type expression interface {
	Evaluate() expression
	String() string
}

type variable struct {
	name string
}

func (v variable) Evaluate() expression {
	return v
}

func (v variable) String() string {
	return v.name
}

type abstraction struct {
	parameter variable
	body      expression
}

func (a abstraction) Evaluate() expression {
	return a
}

func (a abstraction) String() string {
	return fmt.Sprintf("(!%s.%s)", a.parameter, a.body)
}

type application struct {
	left  expression
	right expression
}

func (app application) Evaluate() expression {
	switch left := app.left.(type) {
	case *abstraction:
		return substitute(left.body, left.parameter, app.right).Evaluate()
	case *variable:
		return app
	default:
		panic("Invalid expression")
	}
}

func (app application) String() string {
	return fmt.Sprintf("(%s %s)", app.left, app.right)
}

func substitute(expr expression, _variable variable, value expression) expression {
	switch e := expr.(type) {
	case variable:
		if e == _variable {
			return value
		}
		return e
	case *abstraction:
		if e.parameter.name == _variable.name {
			return e
		}
		return &abstraction{e.parameter, substitute(e.body, _variable, value)}
	case *application:
		return &application{substitute(e.left, _variable, value), substitute(e.right, _variable, value)}
	default:
		panic("Invalid expression")
	}
}

func parseLambdaExpression(expr string) expression {
	stack := lane.NewStack()
	tokens := strings.Fields(expr)

	for _, token := range tokens {
		switch token {
		case "(":
			stack.Push(token)
		case ")":
			args := lane.NewStack()

			for {
				top := stack.Pop()
				if top == "(" {
					break
				}
				args.Prepend(top)
			}

			if args.Size() == 1 {
				stack.Pop() // Discard the opening parentheses
				stack.Push(args.Pop())
			} else {
				funcExpr := stack.Pop().(expression)
				switch funcExpr := funcExpr.(type) {
				case *abstraction:
					parameter := funcExpr.parameter
					body := substitute(funcExpr.body, parameter, args.Pop().(expression))
					stack.Push(&abstraction{parameter, body})
				default:
					stack.Push(&application{funcExpr, args.Pop().(expression)})
				}
			}
		default:
			stack.Push(&variable{name: token})
		}
	}

	return stack.Pop().(expression)
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var request Request
		err := decoder.Decode(&request)

		if err != nil {
			if err.Error() == "EOF" {
				log.Println("Client closed the connection")
				return
			}

			log.Println("Failed to decode request:", err)
			return
		}

		if request.Method == "evaluate" {
			params, ok := request.Params.(map[string]interface{})

			log.Println(params)

			if !ok {
				log.Println("Invalid request parameters")
				return
			}

			expression, ok := params["expression"].(string)
			if !ok {
				log.Println("Invalid expression parameter")
				return
			}

			log.Println(expression)
			express := parseLambdaExpression(expression)
			result := express.Evaluate()
			log.Println(result)

			response := Response{
				ID: request.ID,
				Result: struct {
					Expression string `json:"expression"`
				}{
					Expression: result.String(),
				},
			}

			err = encoder.Encode(response)
			if err != nil {
				log.Println(err)
				if netErr, ok := err.(*net.OpError); ok && netErr.Err.Error() == "write: broken pipe" {
					log.Println("Client closed the connection")
					return
				}

				log.Println("Failed to encode response:", err)
				return
			}

		} else {
			response := Response{
				ID:     request.ID,
				Result: request.Params,
			}

			err = encoder.Encode(response)
			if err != nil {
				log.Println(err)
				if netErr, ok := err.(*net.OpError); ok && netErr.Err.Error() == "write: broken pipe" {
					log.Println("Client closed the connection")
					return
				}

				log.Println("Failed to encode response:", err)
				return
			}
		}

	}
}

func createSocket(socketPath string) error {
	err := os.RemoveAll(socketPath)
	if err != nil {
		return fmt.Errorf("failed to remove existing socket file: %w", err)
	}

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	defer l.Close()

	return nil
}

func cleanupSocket(socketPath string) {
	err := os.RemoveAll(socketPath)
	if err != nil {
		log.Println("Failed to remove socket file:", err)
	}
}
