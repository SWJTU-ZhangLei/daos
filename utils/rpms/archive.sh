#!/bin/bash
#
# Usage: archive.sh <name> <version> <file_dir> <file_extension>
#
# Create an archive of the HEAD commit of a local Git repository, including
# submodules.
#
#   - Must run at the root of the Git repository.
#   - Must have all submodules initialized and updated.
#   - Submodules within submodules are not included.
#   - Must have GNU Tar for the "-A" option.
#   - Supports TMPDIR.
#
# For example,
#
#   archive.sh daos 1.2.3 /a/b/c tar
#
# produces /a/b/c/daos-1.2.3.tar.
#

set -e

# Parse the arguments.
name=$1
version=$2
file_dir=$3
file_ext=$4

# Use a temporary directory for all intermediate files.
tmp=$(mktemp -d)
tmp=$(cd "$tmp" && pwd)
trap 'rm -r "$tmp"' EXIT

# Create the main archive, which does not include any submodule at this point.
file_name=$name-$version
file=$file_name.$file_ext
git archive --prefix "$name-$version/" -o "$tmp/$file" HEAD

# Create one archive for each submodule.
sm_file_name_prefix=$file_name-submodule
git submodule --quiet foreach "git archive --prefix \"$name-$version/\$sm_path/\" -o \"$tmp/$sm_file_name_prefix-\$name.$file_ext\" \$sha1"

# Append all submodule archives to the main archive.
tar -Af "$tmp/$file" "$tmp/$sm_file_name_prefix-"*".$file_ext"

# Publish the main archive.
mv "$tmp/$file" "$file_dir/$file"
