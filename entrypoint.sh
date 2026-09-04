# STARTUP variable is being defined in Pterodactyl egg
MODIFIED_STARTUP=`eval echo $(echo ${STARTUP} | sed -e 's/{{/${/g' -e 's/}}/}/g')`

# Run the Server
exec ${MODIFIED_STARTUP}