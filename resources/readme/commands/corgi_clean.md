# corgi clean

## corgi clean

Cleans all services

### Synopsis

Cleans all db, corgi_services folder, cloned repos, etc.
Useful to clean start corgi as new.
Similar to --fromScratch flag used in other commands, but this is more generic.

Requires items flag.


```
corgi clean [flags]
```

### Examples

```
corgi clean -i all
corgi clean -i db,corgi_services,services
corgi clean -i db
```

### Options

```
  -h, --help            help for clean
  -i, --items strings   Slice of items to clean, like: db,corgi_services,services. 
                        		
                        db - down all databases, that were added to corgi_services folder.
                        corgi_services - clean corgi_services folder.
                        services - delete all services folders (useful, when you want to clean cloned repos folders)
                        
                        all - equal to writing db,corgi_services,services in items
                        		
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
  -o, --runOnce                   Run corgi once and exit
      --silent                    Hide all welcome messages
```

### SEE ALSO

* [corgi](corgi)	 - Corgi cli magic friend

###### Auto generated by spf13/cobra on 7-Apr-2025
