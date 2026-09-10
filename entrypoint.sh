# if STARTUP variable is not being defined in Pterodactyl egg, fallback to a basic STARTUP cmd
if [[ -z "$STARTUP" ]]; then
    export STARTUP="/app/plumber"
fi

MODIFIED_STARTUP=`eval echo $(echo ${STARTUP} | sed -e 's/{{/${/g' -e 's/}}/}/g')`

# Run the Servers
exec ${MODIFIED_STARTUP}