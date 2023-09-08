package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bufio"
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

// Deep copy DbService
func copyDatabaseService(service *utils.DatabaseService) *utils.DatabaseService {
	newService := *service // This performs a shallow copy
	return &newService
}

// Deep copy Service
func copyService(service *utils.Service) *utils.Service {
	newService := *service
	return &newService
}

// Deep copy Required
func copyRequired(req *utils.Required) *utils.Required {
	newReq := *req
	return &newReq
}

func runCreate(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println("Error loading existing configurations:", err)
	} else {
		if corgi.DatabaseServices != nil {
			dbServiceMap := make(map[string]*utils.DatabaseService)
			for _, dvService := range corgi.DatabaseServices {
				name := dvService.ServiceName
				newDbService := copyDatabaseService(&dvService)
				newDbService.ServiceName = ""
				dbServiceMap[name] = newDbService
			}
			configMap[utils.DbServicesInConfig] = dbServiceMap
		}
		if corgi.Services != nil {
			serviceMap := make(map[string]*utils.Service)
			for _, service := range corgi.Services {
				name := service.ServiceName
				newService := copyService(&service)
				newService.ServiceName = ""
				serviceMap[name] = newService
			}
			configMap[utils.ServicesInConfig] = serviceMap
		}
		if corgi.Required != nil {
			requiredMap := make(map[string]*utils.Required)
			for _, req := range corgi.Required {
				name := req.Name
				newReq := copyRequired(&req)
				newReq.Name = ""
				requiredMap[name] = newReq
			}
			configMap[utils.RequiredInConfig] = requiredMap
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
		handleServiceCreation(utils.DbServicesInConfig, &utils.DatabaseService{}, "ServiceName")
	case "Service":
		handleServiceCreation(utils.ServicesInConfig, &utils.Service{}, "ServiceName")
	case "Required":
		handleServiceCreation(utils.RequiredInConfig, &utils.Required{}, "Name")
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
		filename = utils.CorgiComposeDefaultName
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
		optionsTag := field.Tag.Get("options")
		if optionsTag != "" {
			options := strings.Split(optionsTag, ",")
			selectPrompt := promptui.Select{
				Label: prompt,
				Items: options,
			}
			_, selected, err := selectPrompt.Run()
			if err != nil {
				fmt.Printf("Prompt failed %v\n", err)
				return
			}

			if selected == "❌skip" {
				v.Field(i).SetString("")
			} else {
				v.Field(i).SetString(selected)
			}
			continue
		}
		// Check if the field is a struct
		if field.Type.Kind() == reflect.Struct {
			// Ask user if they want to populate this struct
			prompt := promptui.Prompt{
				Label:     fmt.Sprintf("Do you want to populate %s?", field.Name),
				IsConfirm: true,
			}

			_, err := prompt.Run()
			if err != nil { // If user says no or there's an error, skip this struct
				continue
			}

			structField := v.Field(i)
			// Initialize the struct if it's a zero value
			if structField.IsZero() {
				structField.Set(reflect.New(field.Type).Elem())
			}
			askAndSetFields(structField.Addr().Interface())
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
			prompt := formatPrompt(yamlTag, field.Name)
			setUserInputToField(v.Field(i), prompt, field.Name == "ServiceName" || field.Name == "Name")
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

func setUserInputToField(field reflect.Value, prompt string, isRequired bool) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println(prompt)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Failed to read input: %v\n", err)
			return
		}

		input = strings.TrimSpace(input)

		if isRequired && input == "" {
			fmt.Println("This field cannot be empty. Please provide a valid input.")
			continue
		}

		if !isRequired {
			input = strings.ReplaceAll(input, " ", "")
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(input)
		case reflect.Int:
			if value, err := strconv.Atoi(input); err == nil {
				field.SetInt(int64(value))
			}
		}

		break
	}
}

func handleServiceCreation(serviceType string, serviceInstance interface{}, fieldName string) {
	askAndSetFields(serviceInstance)

	nameVal := reflect.ValueOf(serviceInstance).Elem().FieldByName(fieldName).String()
	reflect.ValueOf(serviceInstance).Elem().FieldByName(fieldName).SetString("")

	if _, exists := configMap[serviceType]; !exists {
		switch serviceType {
		case utils.DbServicesInConfig:
			configMap[serviceType] = make(map[string]*utils.DatabaseService)
		case utils.ServicesInConfig:
			configMap[serviceType] = make(map[string]*utils.Service)
		case utils.RequiredInConfig:
			configMap[serviceType] = make(map[string]*utils.Required)
		}
	}

	switch serviceType {
	case utils.DbServicesInConfig:
		servicesMap := configMap[serviceType].(map[string]*utils.DatabaseService)
		servicesMap[nameVal] = serviceInstance.(*utils.DatabaseService)

	case utils.ServicesInConfig:
		servicesMap := configMap[serviceType].(map[string]*utils.Service)
		servicesMap[nameVal] = serviceInstance.(*utils.Service)

	case utils.RequiredInConfig:
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
	encoder.SetIndent(2)
	if dbServiceMap, exists := configMap[utils.DbServicesInConfig]; exists {
		err = encoder.Encode(map[string]interface{}{utils.DbServicesInConfig: dbServiceMap})
		if err != nil {
			fmt.Printf("Error encoding services section: %v\n", err)
		}
	}

	if serviceMap, exists := configMap[utils.ServicesInConfig]; exists {
		err = encoder.Encode(map[string]interface{}{utils.ServicesInConfig: serviceMap})
		if err != nil {
			fmt.Printf("Error encoding dbServices section: %v\n", err)
		}
	}

	if requiredMap, exists := configMap[utils.RequiredInConfig]; exists {
		err = encoder.Encode(map[string]interface{}{utils.RequiredInConfig: requiredMap})
		if err != nil {
			fmt.Printf("Error encoding required section: %v\n", err)
		}
	}

	if err := removeSeparators(filename); err != nil {
		fmt.Printf("Error removing separators: %v\n", err)
		return
	}

	fmt.Printf("%s has been saved successfully!\n", filename)
}

func removeSeparators(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	var lines []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "---" {
			lines = append(lines, line)
		} else {
			lines = append(lines, "")
		}
	}

	if scanner.Err() != nil {
		return scanner.Err()
	}

	// Now write the lines back to the file
	file, err = os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}
	writer.Flush()

	return nil
}
