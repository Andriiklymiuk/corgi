package cmd

import (
	"andriiklymiuk/corgi/utils"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "A command to create configurations for corgi",
	Long:  `A command to interactively prompt the user to create configurations for corgi and save to corgi-compose.yml.`,
	Run:   runCreate,
}

func init() {
	rootCmd.AddCommand(createCmd)
}

var configMap = map[string]interface{}{}

func runCreate(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println("Error loading existing configurations:", err)
	} else {
		if corgi.DatabaseServices != nil {
			dbServiceMap := make(map[string]*utils.DatabaseService)
			for _, service := range corgi.DatabaseServices {
				name := service.ServiceName
				service.ServiceName = ""
				dbServiceMap[name] = &service
			}
			configMap["db_services"] = dbServiceMap
		}
		if corgi.Services != nil {
			serviceMap := make(map[string]*utils.Service)
			for _, service := range corgi.Services {
				name := service.ServiceName
				service.ServiceName = ""
				serviceMap[name] = &service
			}
			configMap["services"] = serviceMap
		}
		if corgi.Required != nil {
			requiredMap := make(map[string]*utils.Required)
			for _, req := range corgi.Required {
				name := req.Name
				req.Name = ""
				requiredMap[name] = &req
			}
			configMap["required"] = requiredMap
		}
	}

	choices := []string{"DatabaseService", "Service", "Required"}
	choice, err := utils.PickItemFromListPrompt("What do you want to create?", choices, "❌ Exit", utils.WithBackStringAtTheEnd())
	if err != nil {
		fmt.Println(err)
		return
	}

	switch choice {
	case "DatabaseService":
		handleServiceCreation("db_services", &utils.DatabaseService{}, "ServiceName")
	case "Service":
		handleServiceCreation("services", &utils.Service{}, "ServiceName")
	case "Required":
		handleServiceCreation("required", &utils.Required{}, "Name")
	}
	prompt := promptui.Prompt{
		Label:     "Do you want to save changes",
		IsConfirm: true,
	}

	_, err = prompt.Run()
	if err != nil {
		return
	}

	filenameFlag, err := cmd.Root().Flags().GetString("filename")
	if err != nil {
		fmt.Print(err.Error())
	}

	var filename string
	if filenameFlag != "" {
		filename = filenameFlag
	} else {
		filename = "corgi-compose.yml"
	}
	saveToFile(filename)

}

func askAndSetFields(item interface{}) {
	v := reflect.ValueOf(item).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		yamlTag := field.Tag.Get("yaml")
		if yamlTag == "-" {
			continue
		}
		prompt := formatPrompt(yamlTag, field.Name)

		if field.Type.Kind() == reflect.Struct {
			askAndSetFields(v.Field(i).Addr().Interface())
		} else if field.Type.Kind() == reflect.Slice {
			sliceType := field.Type.Elem()
			if sliceType.Kind() == reflect.String {
				sliceValues := make([]string, 0)
				fmt.Println(prompt + " (press ENTER after each item; press ENTER with no input when done)")
				for {
					var input string
					fmt.Scanln(&input)
					if input == "" {
						break
					}
					sliceValues = append(sliceValues, input)
				}
				v.Field(i).Set(reflect.ValueOf(sliceValues))
			}
		} else {
			setUserInputToField(v.Field(i), prompt)
		}
	}
}

func formatPrompt(yamlTag, fieldName string) string {
	if yamlTag != "" {
		tagParts := strings.Split(yamlTag, ",")
		return fmt.Sprintf("Enter %s:", tagParts[0])
	}
	return fmt.Sprintf("Enter %s:", strings.ToLower(fieldName))
}

func setUserInputToField(field reflect.Value, prompt string) {
	fmt.Println(prompt)
	var input string
	fmt.Scanln(&input)
	switch field.Kind() {
	case reflect.String:
		field.SetString(input)
	case reflect.Int:
		if value, err := strconv.Atoi(input); err == nil {
			field.SetInt(int64(value))
		}
	}
}

func handleServiceCreation(serviceType string, serviceInstance interface{}, fieldName string) {
	askAndSetFields(serviceInstance)

	nameVal := reflect.ValueOf(serviceInstance).Elem().FieldByName(fieldName).String()
	reflect.ValueOf(serviceInstance).Elem().FieldByName(fieldName).SetString("")

	if _, exists := configMap[serviceType]; !exists {
		switch serviceType {
		case "db_services":
			configMap[serviceType] = make(map[string]*utils.DatabaseService)
		case "services":
			configMap[serviceType] = make(map[string]*utils.Service)
		case "required":
			configMap[serviceType] = make(map[string]*utils.Required)
		}
	}

	switch serviceType {
	case "db_services":
		servicesMap := configMap[serviceType].(map[string]*utils.DatabaseService)
		servicesMap[nameVal] = serviceInstance.(*utils.DatabaseService)

	case "services":
		servicesMap := configMap[serviceType].(map[string]*utils.Service)
		servicesMap[nameVal] = serviceInstance.(*utils.Service)

	case "required":
		servicesMap := configMap[serviceType].(map[string]*utils.Required)
		servicesMap[nameVal] = serviceInstance.(*utils.Required)
	}
}

func saveToFile(filename string) {
	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	err = encoder.Encode(configMap)
	if err != nil {
		fmt.Printf("Error encoding YAML: %v\n", err)
	} else {
		fmt.Printf("%s has been saved successfully!\n", filename)
	}
}
