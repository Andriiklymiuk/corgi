# Create Dump

**👋  How we can create a dump of the database? Let's go guys and follow the guide!**

## Some requirements before starting:

- Docker
- pgAdmin 4
- Your happiness

## Create the dump in pgAdmin 4

- Open your database
- Go to your schema `Databases > ${DATABASE_NAME} > Schemas > Public`
- Click Right on it and Select **Backup**
- `DATABASE_NAME` variable can be replaced by any db name.

### **`General` Tab**

- Fill the field **Filename** as example *dump.sql*
- Fill the field **Format** and select **Plain**

### **`Dump options` Tab**

In `type of objects` section  ✅   `blobs` field

In `do not save` section  ✅   `owner`,`privilege`, `unlogged table data` fields

In `queries` section  ✅   everything, except `Load via partition root`


Thanks to Nicolas for provided info
nicolas.zamarreno@skeepers.io

[Main docs](../../Readme.md)