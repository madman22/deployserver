# deployserver
Web Interface used for deploying programs to systemd based linux servers

This is a fairly specifically made tool to easily deploy executables to a linux server systemd service.  I use it in produciton to update/upload the executable and support files.  Check the wiki for a screenshot.

The only external packages are the storm ORM layer on top of bbolt and gorilla mux.  The javascript and css are links to CDN's.

- main.go: All source code in one file. Sorry!
- service.txt: Template used to create systemd file
- index.html: Templated used for injecting the "Page" struct.  Another sorry for improper template use!

Build, then copy the executable and the two supporting files to /opt/deployserver, run executable with root permissions.  Navigate a web browser to port 8181 on the host.

You can upload a zip of your program.  It will unzip it inside the /opt/deployserver/services/<ServiceName> directory and create a systemd service script, and run reload-daemons to make sure it can be immediatly used with systemd.

Zip format:

"ServiceName".zip<br>
-"ServiceName"Executable<br>
-Any other required files

You can also directly upload files after the initial deployment.  This server will accept the uploaded file, stop the systemd service(if running), copy/replace the file, then start the service(if it was running).


Here is my usual workflow:

1. Server is running and a bug is discovered
2. Fix the bug, recompile program
3. SSH to server, start deployserver, remote forward port through ssh
4. Upload new compiled executable via webpage
5. stop deployserver
6. close ssh
7. wait for the next bug

TODO:
- implement something like https://github.com/kardianos/service for cross platform services
- Fix file browsing/removing
- Make web interface better
- User credentials
- Better formatting of systemd output
- Better handling systemd commands
- accept uploading another zip to batch updates
- sh*t or get off the pot with database integration
  - Add history of user actions
  - archive uploads/backups to database or directories
  - users/groups/etc
- Add one time use functionality for interacting that will still log everything without starting a webserver
