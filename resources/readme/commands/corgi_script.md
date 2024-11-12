# corgi script

## corgi script

Runs script on each service, if it specified

```
corgi script [flags]
```

### Options

```
  -h, --help                        help for script
      --ignore-dependent-services   Ignore dependent services for scripts, while copying env from other services. (default true)
  -n, --names strings               Slice of script names to choose from.
                                    
                                    If you provide at least 1 name here, than corgi will choose only to run these scripts, while ignoring all others.
                                    (--names deploy_staging,test_e2e,smth_smth_script)
                                    
                                    By default all scripts are included to run.
                                    		
      --services strings            Slice of services to choose from.
                                    
                                    If you provide at least 1 services here, than corgi will choose only this service, while ignoring all others.
                                    none - will ignore all services run script.
                                    (--services app,server)
                                    
                                    By default all services are included and script are run on them.
                                    		
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