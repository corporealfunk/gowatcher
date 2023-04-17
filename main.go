package main

import (
  "fmt"
  "strings"
  "os"
  "os/signal"
  "os/exec"
  "io/ioutil"
  "path"
  "path/filepath"
  "log"
  "github.com/fsnotify/fsnotify"
)

/**
 * This program watches a directory for file creation and runs ffmpeg on any files
 * that are added to the directory or files that are present during program start.
 * ENV variables configure FFMPEG and the base directory for the queue:
 * BASE_DIR=/path/to/directory/base
 * FFMPEG_INPUT_FLAGS="flags to ffmpeg before the -i <filename> flag"
 * FFMPEG_OUTPUT_FLAGS="flags to ffmpeg after the -i <filename> flag"
 * Do not include "-i <filename>" in ffmpeg flags, nor the output filename
 * Output files will be placed into "BASE_DIR/finished"
 *
 * The directories under BASE_DIR will be created as follows if they don't exists:
 * ./working       files being encoded are placed here
 * ./finished      encoded files are moved here when completed
 * ./queue         move files here to encode them, this directory is being watched
 * ./holding       if on a remote server, upload files here. when upload
 *                 is complete, move them into ./queue
 *
 * If using on a remote server, processing will start as soon as a file
 * is created, even if a network transport has not completed the file transfer
 * yet. To avoid processing files that have not completely transfered, upload
 * files to the ./holding directory, then move them into ./queue when the
 * upload is complete
 */

func main() {
  // signal interrupts
  interrupt := make(chan os.Signal, 1)
  signal.Notify(interrupt, os.Interrupt)

  // Create a channel of files from the watch directory
  // BASE_DIR=path
  filesChan := make(chan string)

  baseDir := os.Getenv("BASE_DIR")

  exists, err := dirExists(baseDir)

  if err != nil {
    fmt.Fprintf(os.Stderr, "Directory %s error: %s\n", baseDir, err)
    os.Exit(1)
  }

  if !exists {
    fmt.Fprintf(os.Stderr, "Directory %s does not exist\n", err)
    os.Exit(1)
  }

  baseDirAbs, err := filepath.Abs(baseDir)

  if err != nil {
    fmt.Fprintf(os.Stderr, "Filepath ABS error: %s\n", err)
    os.Exit(1)
  }

  // create queue directory
  queueDirAbs := filepath.Join(baseDirAbs, "queue")

  if err = createDir(queueDirAbs); err != nil {
    fmt.Fprintf(os.Stderr, "Error: %s\n", err)
    os.Exit(1)
  }

  // create queue uploadDir
  uploadDirAbs := filepath.Join(baseDirAbs, "upload")

  if err = createDir(uploadDirAbs); err != nil {
    fmt.Fprintf(os.Stderr, "Error: %s\n", err)
    os.Exit(1)
  }

  // create procd/working directory
  workingDirAbs := filepath.Join(baseDirAbs, "working")

  // remove workingDir first
  if err = os.RemoveAll(workingDirAbs); err != nil {
    fmt.Fprintf(os.Stderr, "Error removeing working files: %s\n", err)
    os.Exit(1)
  }

  if err = createDir(workingDirAbs); err != nil {
    fmt.Fprintf(os.Stderr, "Error: %s\n", err)
    os.Exit(1)
  }

  // create procd/finished directory
  finishedDirAbs := filepath.Join(baseDirAbs, "finished")

  if err = createDir(finishedDirAbs); err != nil {
    fmt.Fprintf(os.Stderr, "Error: %s\n", err)
    os.Exit(1)
  }

  // start reading off the channel in a gofunc and running ffmpeg in a child process
  // FFMPEG="-all flags -to ffMPEG"
  ffmpegPath, err := exec.LookPath("ffmpeg")

  if err != nil {
    fmt.Fprintf(os.Stderr, "ffmpeg path error: %s\n", err)
    os.Exit(1)
  }

  ffmpegInputFlags := strings.Fields(os.Getenv("FFMPEG_INPUT_FLAGS"))
  ffmpegOutputFlags := strings.Fields(os.Getenv("FFMPEG_OUTPUT_FLAGS"))

  go func() {
    for file := range filesChan {
      log.Printf("Work on: %s\n", file)

      // always convert to mp4 container
      ext := path.Ext(file)
      outFile := file[0:len(file) - len(ext)] + ".mp4"

      ffmpegCmdFlags := make([]string, 0)

      ffmpegCmdFlags = append(ffmpegCmdFlags, ffmpegInputFlags...)
      ffmpegCmdFlags = append(ffmpegCmdFlags, "-i", file)
      ffmpegCmdFlags = append(ffmpegCmdFlags, ffmpegOutputFlags...)
      workingFilepath := fmt.Sprintf("%s/%s", workingDirAbs, filepath.Base(outFile))
      ffmpegCmdFlags = append(ffmpegCmdFlags, workingFilepath)
      log.Printf("Command: %s\n", ffmpegCmdFlags)

      cmd := exec.Command(ffmpegPath, ffmpegCmdFlags...)
      cmd.Stdout = os.Stdout
      cmd.Stderr = os.Stderr
      if err := cmd.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "FFMPEG Call Error: %s\n", err)
      } else {
        // move file from workingDirAbs to finsihedDirAbs
        finishedFilePath := fmt.Sprintf("%s/%s", finishedDirAbs, filepath.Base(outFile))
        err = os.Rename(workingFilepath, finishedFilePath)

        if err != nil {
          fmt.Fprintf(os.Stderr, "Could not move %s to %s: %s\n", workingFilepath, finishedFilePath, err)
          os.Exit(1)
        }

        // remove the queue original file
        _ = os.Remove(file)
      }
    }
  }()

  log.Printf("Watching %s\n", queueDirAbs)

  // Create new watcher
  watcher, err := fsnotify.NewWatcher()

  if err != nil {
    fmt.Fprintf(os.Stderr, "Watcher Error: %s\n", err)
    os.Exit(1)
  }

  defer watcher.Close()

  // Start listening for events.
  go func() {
    for {
      select {
      case event, ok := <-watcher.Events:
        if !ok {
          return
        }
        // if it's a creation event, send it to the queue channel, but ony if it is not a directory
        // and not a .DotFile
        if event.Has(fsnotify.Create) {
          info, err := os.Stat(event.Name)

          // exists and is not a directory and not .DotFile
          if !os.IsNotExist(err) && !info.IsDir() && string(event.Name[0]) != "." {
            filesChan <- event.Name
          }
        }
      case err, ok := <-watcher.Errors:
        if !ok {
          return
        }
        log.Println("error:", err)
      }
    }
  }()

  // Add a path.
  err = watcher.Add(queueDirAbs)

  if err != nil {
    fmt.Fprintf(os.Stderr, "Watcher.Add() Error: %s\n", err)
    os.Exit(1)
  }

  // process any files that are already in the queue directory
  files, err := ioutil.ReadDir(queueDirAbs)
  if err != nil {
    fmt.Fprintf(os.Stderr, "ReadDir Error: %s\n", err)
    os.Exit(1)
  }

  for _, file := range files {
    if !file.IsDir() && file.Name()[0] != '.' {
      filesChan <- filepath.Join(queueDirAbs, file.Name())
    }
  }


  // run until SIG
  for range interrupt {
    fmt.Println("Interrupted!")
    return
  }
}
