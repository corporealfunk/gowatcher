package main

import (
  "fmt"
  "strings"
  "os"
  "os/signal"
  "os/exec"
  "io/ioutil"
  "path/filepath"
  "log"
  "github.com/fsnotify/fsnotify"
)

/**
 * This program watches a directory for file creation and runs ffmpeg on any files
 * that are added to the directory or files that are present during program start.
 * ENV variables configure FFMPEG and the directory to watch:
 * WATCH_DIR=/path/to/directory/to/watch
 * FFMPEG_INPUT_FLAGS="flags to ffmpeg before the -i <filename> flag"
 * FFMPEG_OUTPUT_FLAGS="flags to ffmpeg after the -i <filename> flag"
 * Do not include "-i <filename>" in ffmpeg flags, nor the output filename
 * Output files will be placed into "WATCH_DIR/procd"
 * If using on a remote server, processing will start as soon as a file
 * is created, even if a network transport has not completed the file transfer
 * yet. To avoid processing files that have not completely transfered, upload
 * files to a separate (or sub directory) from the WATCH_DIR and then copy
 * them into the WATCH_DIR once upload is complete
 */

func main() {
  // signal interrupts
  interrupt := make(chan os.Signal, 1)
  signal.Notify(interrupt, os.Interrupt)

  // Create a channel of files from the watch directory
  // WATCH_DIR=path
  filesChan := make(chan string)

  watchDir := os.Getenv("WATCH_DIR")
  info, err := os.Stat(watchDir)

  if os.IsNotExist(err) || !info.IsDir() {
    fmt.Fprintf(os.Stderr, "Directory %s does not exist\n", watchDir)
    os.Exit(1)
  }
  watchDirAbs, err := filepath.Abs(watchDir)

  if err != nil {
    fmt.Fprintf(os.Stderr, "Filepath Error: %s\n", err)
    os.Exit(1)
  }

  // create procd directory if it doesn't exist
  procdDirAbs := filepath.Join(watchDirAbs, "procd")

  _, err = os.Stat(procdDirAbs)

  if os.IsNotExist(err) {
    if err := os.Mkdir(procdDirAbs, os.ModePerm); err != nil {
      fmt.Fprintf(os.Stderr, "Could not create procd dir Error: %s\n", err)
      os.Exit(1)
    }
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
    for {
      select {
      case file := <-filesChan:
        log.Printf("Work on: %s\n", file)

        ffmpegCmdFlags := make([]string, 0)

        ffmpegCmdFlags = append(ffmpegCmdFlags, ffmpegInputFlags...)
        ffmpegCmdFlags = append(ffmpegCmdFlags, "-i", file)
        ffmpegCmdFlags = append(ffmpegCmdFlags, ffmpegOutputFlags...)
        ffmpegCmdFlags = append(ffmpegCmdFlags, fmt.Sprintf("%s/%s", procdDirAbs, filepath.Base(file)))
        log.Printf("Command: %s\n", ffmpegCmdFlags)

        cmd := exec.Command(ffmpegPath, ffmpegCmdFlags...)
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr
        if err := cmd.Run(); err != nil {
          fmt.Fprintf(os.Stderr, "FFMPEG Call Error: %s\n", err)
        }
      }
    }
  }()

  log.Printf("Watching %s\n", watchDirAbs)

  files, err := ioutil.ReadDir(watchDirAbs)
  if err != nil {
    fmt.Fprintf(os.Stderr, "ReadDir Error: %s\n", err)
    os.Exit(1)
  }

  for _, file := range files {
    if !file.IsDir() && file.Name()[0] != '.' {
      filesChan <- filepath.Join(watchDirAbs, file.Name())
    }
  }

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
  err = watcher.Add(watchDirAbs)

  if err != nil {
    fmt.Fprintf(os.Stderr, "Watcher.Add() Error: %s\n", err)
    os.Exit(1)
  }

  // run until SIG
  for {
    select {
    case <-interrupt:
      fmt.Println("Interrupted!")
    }
    return
  }
}
