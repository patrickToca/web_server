# for prod
docker run --rm -it \
  --env-file .env.docker.local \
  --add-host host.docker.internal:host-gateway \
  -p 8080:8080 \
  patricktoca/mywebapp:v0.8.4_20261010

# The --add-host flag is required on Linux for host.docker.internal to resolve to the host.
# On Docker Desktop it is added automatically; on Docker Engine you need the flag.


#=========================================
# for dev
docker run --rm -it \
  --env-file .env.docker.local \
  -e APP_ENV=development \
  --add-host host.docker.internal:host-gateway \
  -p 8080:8080 \
  patricktoca/mywebapp:v0.8.4_20261010

#   The -e APP_ENV=development after --env-file overrides the value in the file.
#   Docker's flag precedence is "last one wins", and -e comes after the file.