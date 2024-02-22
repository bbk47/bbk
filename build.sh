#!/usr/bin/env bash

bbk_VERSION=`git describe --tags --abbrev=0`
GIT_HASH=`git rev-parse --short HEAD`
echo $bbk_VERSION'-'$GIT_HASH

cwd=`pwd`


rm -rf ./output
mkdir -p ./output

# os_all='linux windows darwin freebsd'
# arch_all='386 amd64 arm arm64'

platforms=( "darwin/amd64" )

for platform in "${platforms[@]}"
do
    platform_split=(${platform//\// })
    os=${platform_split[0]}
    arch=${platform_split[1]}

    targetzipname="bbk_${bbk_VERSION}_${os}_${arch}"
    bbk_outdir="output/${targetzipname}"
    output_name1=bbk
    mkdir -p $bbk_outdir
    if [ $os = "windows" ]; then
        output_name1+='.exe'
        output_name2+='.exe'
    fi
    echo "Build output/${targetzipname}...";\
    env CGO_ENABLED=0 GOOS=${os} GOARCH=${arch} go build -trimpath -ldflags "-X main.Version=$bbk_VERSION -X main.GitCommitHash=$GIT_HASH" -o ${bbk_outdir}/${output_name1} ./main.go
    echo "Build ${bbk_outdir} done";

    cp -rf ./etc ${bbk_outdir}
    cp -rf ./examples ${bbk_outdir}
    cp -rf ./daemon ${bbk_outdir}

    # packages
    cd output
    if [ $os = "windows" ]; then
        zip -rq ${targetzipname}.zip ${targetzipname}
    else
        tar -zcf ${targetzipname}.tar.gz ${targetzipname}
    fi  
    rm -rf ${targetzipname}
    cd ..
done

cd -