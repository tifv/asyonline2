SCRIPT_NAME="test/nginx-run.sh"

if [[ "$0" != "$SCRIPT_NAME" ]]
then
    echo "This script must be started as $SCRIPT_NAME" >&2
    exit 1
fi

if [[ -z "$1" ]]
then
    echo "Put a temporary directory for nginx as a first argument" >&2
    exit 1
fi

if [[ "$PWD" == *"|"* ]]
then
    echo "Current directory cannot contain “|”" >&2
    exit 1
fi

if [[ "$1" == *"|"* ]]
then
    echo "Temporary directory cannot contain “|”" >&2
    exit 1
fi

sed -e "s|%NGINXDIR%|$1|" -e "s|%CLIENTDIR%|$PWD|" < "$PWD"/test/nginx.conf > $1/nginx.conf

nginx -c $1/nginx.conf
