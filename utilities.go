package main

import (
  "fmt"
  "os"
)

func dirExists(dirName string) (bool, error) {
  info, err := os.Stat(dirName)

  if err != nil && !os.IsNotExist(err) {
    return false, err
  }

  if os.IsNotExist(err) || !info.IsDir() {
    return false, nil
  }

  return true, nil
}

func createDir(dirName string) error {
  exists, err := dirExists(dirName)

  if err != nil {
    return err
  }

  if exists {
    return nil
  }

  if err := os.Mkdir(dirName, os.ModePerm); err != nil {
    return fmt.Errorf("Could not create dir: %s\n", err)
  }

  return nil
}
