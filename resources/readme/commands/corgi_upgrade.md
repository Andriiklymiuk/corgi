# corgi upgrade

## corgi upgrade

Upgrade corgi to the latest version

### Synopsis

Use this command to upgrade corgi to the latest version available in Homebrew.

```
corgi upgrade [flags]
```

### Options

```
  -h, --help   help for upgrade
```

### Options inherited from parent commands

```
      --describe                  Describe contents of corgi-compose file
      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")
  -l, --exampleList               List examples to choose from. Click on any example to download it
  -f, --filename string           Custom filepath for for corgi-compose
      --fromScratch               Clean corgi_services folder before running
  -t, --fromTemplate string       Create corgi service from template url
      --fromTemplateName string   Create corgi service from template name and url
  -g, --global                    Use global path to one of the services
      --privateToken string       Private token for private repositories to download files
      --runOnce                   Run corgi once and exit
      --silent                    Hide all welcome messages
```

### SEE ALSO

* [corgi](corgi)	 - Corgi cli magic friend

###### Auto generated by spf13/cobra on 2-Sep-2024