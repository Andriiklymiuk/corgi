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
	Use:     "create",
	Short:   "A command to create configurations for corgi",
	Long:    `A command to interactively prompt the user to create configurations for corgi and save to corgi-compose.yml.`,
	Run:     runCreate,
	Aliases: []string{"add", "new"},
}

func init() {
	rootCmd.AddCommand(createCmd)
}

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

func lowercaseFirstLetter(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]+'a'-'A') + s[1:]
}

func GetCorgiServicesMap(corgi *utils.CorgiCompose) map[string]interface{} {
	corgiServicesMap := map[string]interface{}{}
	addDbServicesToMap(corgi, corgiServicesMap)
	addServicesToMap(corgi, corgiServicesMap)
	addRequiredToMap(corgi, corgiServicesMap)
	addLifecycleToMap(corgi, corgiServicesMap)
	addFlagsToMap(corgi, corgiServicesMap)
	if corgi.Name != "" {
		corgiServicesMap[utils.NameInConfig] = corgi.Name
	}
	if corgi.Description != "" {
		corgiServicesMap[utils.DescriptionInConfig] = corgi.Description
	}

	return corgiServicesMap
}

func addDbServicesToMap(corgi *utils.CorgiCompose, m map[string]interface{}) {
	if corgi.DatabaseServices == nil {
		return
	}
	dbServiceMap := make(map[string]*utils.DatabaseService)
	for _, dvService := range corgi.DatabaseServices {
		name := dvService.ServiceName
		newDbService := copyDatabaseService(&dvService)
		newDbService.ServiceName = ""
		dbServiceMap[name] = newDbService
	}
	m[utils.DbServicesInConfig] = dbServiceMap
}

func addServicesToMap(corgi *utils.CorgiCompose, m map[string]interface{}) {
	if corgi.Services == nil {
		return
	}
	serviceMap := make(map[string]*utils.Service)
	for _, service := range corgi.Services {
		name := service.ServiceName
		newService := copyService(&service)
		newService.ServiceName = ""
		serviceMap[name] = newService
	}
	m[utils.ServicesInConfig] = serviceMap
}

func addRequiredToMap(corgi *utils.CorgiCompose, m map[string]interface{}) {
	if corgi.Required == nil {
		return
	}
	requiredMap := make(map[string]*utils.Required)
	for _, req := range corgi.Required {
		name := req.Name
		newReq := copyRequired(&req)
		newReq.Name = ""
		requiredMap[name] = newReq
	}
	m[utils.RequiredInConfig] = requiredMap
}

func addLifecycleToMap(corgi *utils.CorgiCompose, m map[string]interface{}) {
	if corgi.Init != nil {
		m[utils.InitInConfig] = corgi.Init
	}
	if corgi.Start != nil {
		m[utils.StartInConfig] = corgi.Start
	}
	if corgi.BeforeStart != nil {
		m[utils.BeforeStartInConfig] = corgi.BeforeStart
	}
	if corgi.AfterStart != nil {
		m[utils.AfterStartInConfig] = corgi.AfterStart
	}
}

func addFlagsToMap(corgi *utils.CorgiCompose, m map[string]interface{}) {
	if corgi.UseDocker {
		m[utils.UseDockerInConfig] = corgi.UseDocker
	}
	if corgi.UseAwsVpn {
		m[utils.UseAwsVpnInConfig] = corgi.UseAwsVpn
	}
}

func runCreate(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	var corgiMap map[string]interface{}

	if err != nil {
		fmt.Println("Error loading existing configurations:", err)
		corgiMap = map[string]interface{}{}
	} else {
		corgiMap = GetCorgiServicesMap(corgi)
	}

	choices := []string{
		"DatabaseService",
		"Service",
		"Required",
		"Init",
		"BeforeStart",
		"Start",
		"AfterStart",
	}
	choice, err := utils.PickItemFromListPrompt("What do you want to create?", choices, "❌ Exit", utils.WithBackStringAtTheEnd())
	if err != nil {
		fmt.Println(err)
		return
	}

	switch choice {
	case "DatabaseService":
		handleServiceCreation(
			corgiMap,
			utils.DbServicesInConfig,
			&utils.DatabaseService{},
			"ServiceName",
		)
	case "Service":
		handleServiceCreation(
			corgiMap,
			utils.ServicesInConfig,
			&utils.Service{},
			"ServiceName",
		)
	case "Required":
		handleServiceCreation(
			corgiMap,
			utils.RequiredInConfig,
			&utils.Required{},
			"Name",
		)

	case
		"Init",
		"BeforeStart",
		"Start",
		"AfterStart":
		handleCommandCreation(
			corgiMap,
			lowercaseFirstLetter(choice),
		)
	}
	prompt := promptui.Prompt{
		Label:     "Do you want to save changes",
		IsConfirm: true,
	}

	_, err = prompt.Run()
	if err != nil {
		return
	}

	UpdateCorgiComposeFileWithMap(corgiMap)
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
		if !askField(v, i, field, yamlTag) {
			return
		}
	}
}

func askField(v reflect.Value, i int, field reflect.StructField, yamlTag string) bool {
	prompt := formatPrompt(yamlTag, field.Name)
	if optionsTag := field.Tag.Get("options"); optionsTag != "" {
		return promptOptions(v.Field(i), prompt, optionsTag)
	}
	switch field.Type.Kind() {
	case reflect.Struct:
		promptStructField(v.Field(i), field)
	case reflect.Ptr:
		promptPtrField(v.Field(i), field, prompt)
	case reflect.Slice:
		promptSliceField(v.Field(i), field, prompt)
	case reflect.Map:
		promptMapField(v.Field(i), field, prompt)
	default:
		setUserInputToField(v.Field(i), prompt, field.Name == "ServiceName" || field.Name == "Name")
	}
	return true
}

func promptOptions(field reflect.Value, prompt, optionsTag string) bool {
	options := strings.Split(optionsTag, ",")
	selectPrompt := promptui.Select{Label: prompt, Items: options}
	_, selected, err := selectPrompt.Run()
	if err != nil {
		fmt.Printf("Prompt failed %v\n", err)
		return false
	}
	if selected == "❌skip" {
		field.SetString("")
	} else {
		field.SetString(selected)
	}
	return true
}

func confirmPopulate(name string) bool {
	prompt := promptui.Prompt{
		Label:     fmt.Sprintf("Do you want to populate %s?", name),
		IsConfirm: true,
	}
	_, err := prompt.Run()
	return err == nil
}

func promptStructField(structField reflect.Value, field reflect.StructField) {
	if !confirmPopulate(field.Name) {
		return
	}
	if structField.IsZero() {
		structField.Set(reflect.New(field.Type).Elem())
	}
	askAndSetFields(structField.Addr().Interface())
}

func promptPtrField(target reflect.Value, field reflect.StructField, prompt string) {
	if !confirmPopulate(field.Name) {
		return
	}
	elemType := field.Type.Elem()
	ptr := reflect.New(elemType)
	switch elemType.Kind() {
	case reflect.Struct:
		askAndSetFields(ptr.Interface())
	case reflect.Bool:
		yn := promptui.Prompt{
			Label:     fmt.Sprintf("Set %s to true?", field.Name),
			IsConfirm: true,
		}
		_, err := yn.Run()
		ptr.Elem().SetBool(err == nil)
	case reflect.String, reflect.Int:
		setUserInputToField(ptr.Elem(), prompt, false)
	default:
		return
	}
	target.Set(ptr)
}

func promptSliceField(target reflect.Value, field reflect.StructField, prompt string) {
	sliceType := field.Type.Elem()
	switch sliceType.Kind() {
	case reflect.String:
		target.Set(reflect.ValueOf(readStringSlice(prompt)))
	case reflect.Struct:
		slice := readStructSlice(field.Type, sliceType, field.Name)
		if slice.Len() > 0 {
			target.Set(slice)
		}
	default:
		fmt.Printf("(skipping %s — edit corgi-compose.yml directly for this field)\n", field.Name)
	}
}

func readStringSlice(prompt string) []string {
	fmt.Println(prompt + " (press ENTER after each item; press ENTER with no input when done)")
	scanner := bufio.NewScanner(os.Stdin)
	var commands []string
	for scanner.Scan() {
		command := scanner.Text()
		if command == "" {
			break
		}
		commands = append(commands, command)
	}
	return commands
}

func readStructSlice(fieldType, sliceType reflect.Type, fieldName string) reflect.Value {
	slice := reflect.MakeSlice(fieldType, 0, 0)
	for {
		more := promptui.Prompt{
			Label:     fmt.Sprintf("Add an entry to %s?", fieldName),
			IsConfirm: true,
		}
		if _, err := more.Run(); err != nil {
			break
		}
		elem := reflect.New(sliceType)
		askAndSetFields(elem.Interface())
		slice = reflect.Append(slice, elem.Elem())
	}
	return slice
}

func promptMapField(target reflect.Value, field reflect.StructField, prompt string) {
	fmt.Println(prompt + " (enter as key=value, ENTER on empty line to finish)")
	scanner := bufio.NewScanner(os.Stdin)
	out := reflect.MakeMap(field.Type)
	valueKind := field.Type.Elem().Kind()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			break
		}
		k, val, ok := parseMapEntry(line, valueKind)
		if !ok {
			continue
		}
		out.SetMapIndex(k, val)
	}
	if out.Len() > 0 {
		target.Set(out)
	}
}

func parseMapEntry(line string, valueKind reflect.Kind) (reflect.Value, reflect.Value, bool) {
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		fmt.Println("  expected key=value, skipping line")
		return reflect.Value{}, reflect.Value{}, false
	}
	k := reflect.ValueOf(strings.TrimSpace(line[:eq]))
	rawVal := strings.TrimSpace(line[eq+1:])
	switch valueKind {
	case reflect.String, reflect.Interface:
		return k, reflect.ValueOf(rawVal), true
	default:
		fmt.Printf("  unsupported map value kind %s, skipping\n", valueKind)
		return reflect.Value{}, reflect.Value{}, false
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

func handleCommandCreation(corgiMap map[string]interface{}, section string) {
	fmt.Printf("Enter commands for %s section (empty input to finish):\n", section)

	var commands []string
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		command := scanner.Text()
		if command == "" {
			break
		}
		commands = append(commands, command)
	}

	existingCommands, exists := corgiMap[section].([]string)
	if exists {
		commands = append(existingCommands, commands...)
	}

	corgiMap[section] = commands

	fmt.Printf("Commands for %s section have been saved successfully!\n", section)
}

func handleServiceCreation(corgiMap map[string]interface{}, serviceType string, serviceInstance interface{}, fieldName string) {
	askAndSetFields(serviceInstance)

	nameVal := reflect.ValueOf(serviceInstance).Elem().FieldByName(fieldName).String()
	reflect.ValueOf(serviceInstance).Elem().FieldByName(fieldName).SetString("")

	if _, exists := corgiMap[serviceType]; !exists {
		switch serviceType {
		case utils.DbServicesInConfig:
			corgiMap[serviceType] = make(map[string]*utils.DatabaseService)
		case utils.ServicesInConfig:
			corgiMap[serviceType] = make(map[string]*utils.Service)
		case utils.RequiredInConfig:
			corgiMap[serviceType] = make(map[string]*utils.Required)
		}
	}

	switch serviceType {
	case utils.DbServicesInConfig:
		servicesMap := corgiMap[serviceType].(map[string]*utils.DatabaseService)
		servicesMap[nameVal] = serviceInstance.(*utils.DatabaseService)

	case utils.ServicesInConfig:
		servicesMap := corgiMap[serviceType].(map[string]*utils.Service)
		servicesMap[nameVal] = serviceInstance.(*utils.Service)

	case utils.RequiredInConfig:
		servicesMap := corgiMap[serviceType].(map[string]*utils.Required)
		servicesMap[nameVal] = serviceInstance.(*utils.Required)
	}
}

func UpdateCorgiComposeFileWithMap(corgiMap map[string]interface{}) {
	var filename string
	if utils.CorgiComposePath != "" {
		filename = utils.CorgiComposePath
	} else {
		filename = utils.CorgiComposeDefaultName
	}

	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)

	encodeSection(encoder, corgiMap, utils.DbServicesInConfig, "services")
	encodeScalarSections(encoder, corgiMap)
	encodeSection(encoder, corgiMap, utils.ServicesInConfig, "dbServices")
	encodeSection(encoder, corgiMap, utils.RequiredInConfig, "required")

	if err := removeSeparators(filename); err != nil {
		fmt.Printf("Error removing separators: %v\n", err)
		return
	}

	fmt.Printf("%s has been saved successfully!\n", filename)
}

func encodeSection(encoder *yaml.Encoder, corgiMap map[string]interface{}, key, label string) {
	section, exists := corgiMap[key]
	if !exists {
		return
	}
	if err := encoder.Encode(map[string]interface{}{key: section}); err != nil {
		fmt.Printf("Error encoding %s section: %v\n", label, err)
	}
}

func encodeScalarSections(encoder *yaml.Encoder, corgiMap map[string]interface{}) {
	scalarKeys := []string{
		utils.InitInConfig,
		utils.StartInConfig,
		utils.BeforeStartInConfig,
		utils.AfterStartInConfig,
		utils.UseDockerInConfig,
		utils.UseAwsVpnInConfig,
		utils.NameInConfig,
		utils.DescriptionInConfig,
	}
	for _, sectionKey := range scalarKeys {
		section, exists := corgiMap[sectionKey]
		if !exists {
			continue
		}
		if sectionArr, ok := section.([]string); ok && len(sectionArr) == 0 {
			continue
		}
		if err := encoder.Encode(map[string]interface{}{sectionKey: section}); err != nil {
			fmt.Printf("Error encoding %s section: %v\n", sectionKey, err)
		}
	}
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
