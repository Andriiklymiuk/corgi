package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"andriiklymiuk/corgi/cmd"
	"andriiklymiuk/corgi/utils"

	"github.com/manifoldco/promptui"
)

func main() {
	ClearTerminal()
	showWelcomeMessage()
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
	if !canShowWelcomeMessages() {
		return
	}
	fmt.Println(
		"\n✨ Glad for using me ✨ See you next time 🚀 🐶",
		string("\n\n\033[36m"),
		utils.GetRandomQuote("famous-quotes"),
		utils.WhiteColor,
	)
}

func canRunCliAgain(cmdExecuted string) bool {
	if cmdExecuted == "corgi" {
		return false
	}

	for _, arg := range os.Args {
		if arg == "init" ||
			arg == "run" ||
			arg == "clean" ||
			arg == "docs" ||
			arg == "filename" {
			return false
		}
		if arg[0:1] == "-" && arg != "-f" {
			return false
		}
	}
	return true
}

func canShowWelcomeMessages() bool {
	for _, arg := range os.Args {
		if arg == "docs" ||
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
	fmt.Println(utils.YellowColor, art, utils.WhiteColor)
	fmt.Println(`🐶 WOOF CORGI 🐶 says:`)
	fmt.Println(utils.CyanColor, utils.GetRandomQuote("famous-quotes"), utils.WhiteColor)
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
