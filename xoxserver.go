package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type client struct {
	con      *websocket.Conn
	name     string
	id       int
	roomNum  int
	roomPos  int
	conState bool
	message  chan string
}

type room struct {
	user [2]*client
}

var rooms [200]room

var numOfPlayers int
var id int = 0

var addr = flag.String("addr", ":3005", "http service address")

var upgrader = websocket.Upgrader{} // use default options

func echo(w http.ResponseWriter, r *http.Request) {

	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()

	numOfPlayers++
	id++

	if id == 0 {
		id++
	}

	var clientName string
	messChan := make(chan string)

	var user client = client{
		c,
		clientName,
		id,
		-1,
		-1,
		true,
		messChan,
	}

	for clientName == "" { //Listen for messages until "init <name>"
		_, message, err := c.ReadMessage()
		if err != nil {
			numOfPlayers--
			user.conState = false
			break
		}

		if strings.Split(string(message), " ")[0] == "init" {
			if len(clientName) <= 5 {
				clientName = "Empty"
			}
			clientName = string(message)[5:]
			if clientName == "" || clientName == "Waiting" {
				clientName = "Empty"
			}
			user.name = clientName
			break
		}
	}

	gameModerator(&user)

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			numOfPlayers--
			user.conState = false
			break
		}
		user.message <- string(message)
	}

}

func gameModerator(user *client) {
	var foundSpot bool = false

	if user.roomNum == -1 {
		for i := 0; i < len(rooms); i++ {
			for x := 0; x < len(rooms[i].user); x++ {
				if rooms[i].user[x] == nil {
					foundSpot = true
				} else if rooms[i].user[x].conState == false {
					foundSpot = true
				}
				if foundSpot {
					user.roomNum = i
					user.roomPos = x
					rooms[i].user[x] = user
					break
				}
			}
			if foundSpot { //Stop searching if found an empty spot
				fmt.Println(user, " Current number of players:", numOfPlayers)
				break
			}
		}
	}

	if foundSpot == false {
		user.con.WriteMessage(1, []byte("Sorry! We couldn't find an empty spot"))
		return
	}

	var fullRoom bool = true

	for i := 0; i < len(rooms[0].user); i++ { //Check if the currrent user's room is full
		if rooms[user.roomNum].user[i] == nil {
			fullRoom = false
		} else if rooms[user.roomNum].user[i].conState == false {
			fullRoom = false
		}
	}

	if fullRoom { //If the room is full assign a manager to serve it
		go roomManager(user.roomNum)
	}
}

func roomManager(roomNum int) {

	var board [9]int
	const boardWidth int = 3

	var turn int = 0
	var users [len(rooms[roomNum].user)]*client = rooms[roomNum].user

	for i := 0; i < len(users); i++ {
		if users[i].conState == false {
			break
		}
	}

	err := users[0].con.WriteMessage(1, []byte("op "+users[1].name+" "+"0"+" "+strconv.Itoa(turn))) //Format is "op <opponentName> <playerTurnID> <currentTurn>"
	if err != nil {
		users[0].conState = false
	}
	err = users[1].con.WriteMessage(1, []byte("op "+users[0].name+" "+"1"+" "+strconv.Itoa(turn)))
	if err != nil {
		users[1].conState = false
	}

	//Clear buffers

	var boardChanged bool = false

	for {
		select {
		case userMess := <-users[0].message:
			const playerNum int = 0
			boardChanged = processMessage(userMess, playerNum, &turn, &board, &rooms[roomNum])
		case userMess := <-users[1].message:
			const playerNum int = 1
			boardChanged = processMessage(userMess, playerNum, &turn, &board, &rooms[roomNum])
		default:
			time.Sleep(time.Second * 1)
		}

		//Reset the board if there are no empty slots left
		var foundEmpty bool = false
		if boardChanged { //Only check if the board was changed this loop
			for i := 0; i < len(board); i++ {
				if board[i] == 0 {
					foundEmpty = true
					break
				}
			}
			if foundEmpty == false {
				resetBoards(&rooms[roomNum], &board)
			}
			boardChanged = false
		}

		//Kill roomManager if anyone in the room is dead >:)
		var anyDead bool = false
		for i := 0; i < len(users); i++ {
			if users[i].conState == false {
				anyDead = true
			}
		}
		if anyDead == true {
			break
		}
	}
}

func makeMove(userMess string, playerNum int, board *[9]int) ([2]int, bool) {

	var unSuccessfull bool = false
	const boardWidth int = 3
	const boardHeight int = 3

	coords := strings.Split(userMess, " ") // [0] is mv [1] is X [2] is Y
	if len(coords) >= 3 {                  //Check if the input is mangled
		xCoord, err := strconv.Atoi(coords[1])
		if err != nil {
			unSuccessfull = true
		}
		if xCoord >= boardWidth {
			unSuccessfull = true
		}
		yCoord, err := strconv.Atoi(coords[2])
		if yCoord >= boardHeight {
			unSuccessfull = true
		}
		if err != nil {
			unSuccessfull = true
		}
		if unSuccessfull == false {
			if board[yCoord*boardWidth+xCoord] == 0 {
				board[yCoord*boardWidth+xCoord] = playerNum + 1 // This has to playerNum + 1 because 0 is for null
				return [2]int{xCoord, yCoord}, true
			}
		}
	}
	return [2]int{0, 0}, false
}

func processMessage(userMess string, playerNum int, turn *int, board *[9]int, roomf *room) (boardChanged bool) {

	boardChanged = false

	if len(userMess) > 2 {
		if userMess[:2] == "mv" {
			if *turn == playerNum {
				coords, success := makeMove(userMess, playerNum, board)
				if success {
					msg := "mvs " + strconv.Itoa(playerNum+1) + " " + strconv.Itoa(coords[0]) + " " + strconv.Itoa(coords[1]) // "mvs <playernumber+1> <xCord> <yCord>"
					for i := 0; i < len(roomf.user); i++ {
						roomf.user[i].con.WriteMessage(1, []byte(msg))
					}
					boardChanged = true

					if *turn == 0 {
						*turn = 1
					} else {
						*turn = 0
					}

				}
			}
		}
	}

	return
}

func resetBoards(roomToReset *room, board *[9]int) {
	for i := 0; i < len(roomToReset.user); i++ {
		roomToReset.user[i].con.WriteMessage(1, []byte("resetBoard"))
	}
	for i := 0; i < len(board); i++ {
		board[i] = 0
	}
}

func main() {

	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/echo", echo)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "xox.html")
	})
	log.Fatal(http.ListenAndServe(*addr, nil))

}
