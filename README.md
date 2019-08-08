# deployserver
Web Interface used for deploying programs to systemd based linux servers

This is a fairly specifically made tool to easily deploy executables to a linux server systemd service.  I use it in produciton to update/upload the executable and support files.


Copy to /opt/deployserver, run executable with root permissions.  Navigate a web browser to port 8181 on the host.

You can upload a zip of your program.  It will unzip it inside the /opt/deployserver/services/<ServiceName> directory and create a systemd service script, and run reload-daemons to make sure it can be immediatly used with systemd.

Zip format:

<ServiceName>.zip
-<ServiceNameExecutable>
-Any other required files

You can also directly upload files after the initial deployment.  This server will accept the uploaded file, stop the systemd service(if running), copy/replace the file, then start the service(if it was running).


Here is my usual workflow:

Server is running and a bug is discovered
Fix the bug, recompile program
SSH to server, start deployserver, remote forward port through ssh
Upload new compiled executable via webpage
stop deployserver
close ssh
wait for the next bug

TODO:
Fix file browsing/removing
Make web interface better
User credentials
Better formatting of systemd output
Better handling systemd commands
