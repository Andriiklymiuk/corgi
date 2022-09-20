package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/manifoldco/promptui"
	"andriiklymiuk/corgi/cmd"
	"andriiklymiuk/corgi/utils"
)

func main() {
	ClearTerminal()
	welcomeBanner()
	var runCli func()
	runCli = func() {
		cmdExecuted := cmd.Execute()

		canRunCli := canRunCliAgain(cmdExecuted)
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
	randomQuote := utils.GetRandomQuote("technology")
	fmt.Printf(`
Glad for using me ✨ See you next time 🚀 🐶

%s

					`,
		randomQuote,
	)
}

func canRunCliAgain(cmdExecuted string) bool {
	if cmdExecuted == "corgi" {
		return false
	}
	lastWordInCmd := os.Args[len(os.Args)-1]
	if lastWordInCmd == "init" || lastWordInCmd == "run" {
		return false
	}
	if lastWordInCmd[0:1] == "-" {
		return false
	}
	return true
}

func welcomeBanner() {
	art := `                             
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
	fmt.Println(string("\033[33m"), art, string("\033[0m"))
	fmt.Println(`🐶 WOOF CORGI 🐶 says:`)
	fmt.Println(utils.GetRandomQuote("famous-quotes"))
	fmt.Println()
}

func runClearCmd(name string, arg ...string) {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func ClearTerminal() {
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
