package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"andriiklymiuk/corgi/cmd"
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"

	"github.com/manifoldco/promptui"
)

func main() {
	ClearTerminal()
	showWelcomeMessage()
	var runCli func()
	runCli = func() {
		cmd.Execute()

		canRunCli := canRunCliAgain()
		if !canRunCli {
			showFinalMessage()
			return
		}

		prompt := promptui.Prompt{
			Label:     "Do you want to continue using Corgi?",
			IsConfirm: true,
		}

		result, err := prompt.Run()
		if err != nil {
			showFinalMessage()
			return
		}

		fmt.Printf("You choose, so here we go again %q\n", result)
		runCli()
	}
	runCli()
}

func showFinalMessage() {
	if !canShowWelcomeMessages() {
		return
	}
	utils.PrintFinalMessage()
}

func canRunCliAgain() bool {
	var hasDbCmd bool
	for _, arg := range os.Args {
		if arg == "-f" || arg == "--filename" {
			continue
		}
		if arg[0:1] == "-" {
			return false
		}
		if arg == "db" {
			hasDbCmd = true
		}
	}
	return hasDbCmd
}

func canShowWelcomeMessages() bool {
	for _, arg := range os.Args {
		if arg == "docs" ||
			arg == "doctor" ||
			arg == "--silent" ||
			arg == "--version" ||
			arg == "-v" ||
			arg == "-h" ||
			arg == "--help" {
			return false
		}
	}
	return true
}

func showWelcomeMessage() {
	if !canShowWelcomeMessages() {
		return
	}
	artImage := `                             
                @@                                
              ******@                             
             @*******@                            
             &********@              @*****@      
             @*********%@@@@      &********/      
             @*****************************@      
             @****************************@       
              @******/&@@@@**************@        
               @*****************@******@         
            @***********************@*#           
          @*******@********&**********@           
        /*****     /*&****.       ******@         
        *****         %(           ******@        
       @*****                      *******,       
       (*****                      *******&       
       @******                   ,*******&        
        (*******                ********@         
         @********      .    *********&           
             @#*****@@    @@@@&%@@@@              
             &&     ,      @      &    
                                                           
`
	fmt.Println(art.YellowColor, artImage, art.WhiteColor)
	fmt.Println(`🐶 WOOF CORGI 🐶 says:`)
	fmt.Println(art.CyanColor, utils.GetRandomQuote(), art.WhiteColor)
	fmt.Println()
}

func runClearCmd(name string, arg ...string) {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func ClearTerminal() {
	for _, arg := range os.Args {
		if arg == "--silent" {
			return
		}
	}
	switch runtime.GOOS {
	case "darwin":
		runClearCmd("tput", "reset")
	case "linux":
		runClearCmd("clear")
	case "windows":
		runClearCmd("cmd", "/c", "cls")
	default:
		runClearCmd("clear")
	}
}
