# Use the official Go image. A specific version ensures reproducibility.
FROM golang:1.24.5-alpine

# Set a default working directory. The 'docker run -w /mnt' flag will override
# this at runtime, but it's good practice to have a default.
WORKDIR /app

# Expose port 9090, as required for the network service.
EXPOSE 9090

# This ENTRYPOINT is the key to solving the permission issue without an external file.
# It runs a shell command that first fixes permissions, then executes the command
# you provide in 'docker run'.
#
# How it works:
# 1. ["/bin/sh", "-c", "..."] starts a shell to run the command string.
# 2. "if [ -d /mnt/scripts ]; then chmod +x /mnt/scripts/*.sh; fi;"
#    This safely finds all .sh files in the mounted /mnt/scripts directory
#    and makes them executable. It handles cases where the directory might not exist.
# 3. "exec \"$@\"" This executes the command passed to the container (e.g., './scripts/build.sh').
# 4. The final "sh" is a placeholder for the script name ($0), allowing the rest of the
#    arguments from 'docker run' to be correctly passed to the script via '$@'. This is
#    the crucial detail that makes this solution robust.
ENTRYPOINT ["/bin/sh", "-c", "if [ -d /mnt/scripts ]; then chmod +x /mnt/scripts/*.sh; fi; exec \"$@\"", "sh"]

# Set a default command. This will be overridden by your 'docker run' command.
# If you were to run 'docker run mytest' without a command, it would start an interactive shell.
CMD ["/bin/sh"]
