package sync

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/devspace-cloud/devspace/helper/util"
	"github.com/devspace-cloud/devspace/pkg/util/log"

	"github.com/pkg/errors"
	gitignore "github.com/sabhiram/go-gitignore"
)

// Unarchiver is responsible for unarchiving a remote archive
type Unarchiver struct {
	syncConfig    *Sync
	forceOverride bool

	log log.Logger
}

// NewUnarchiver creates a new unarchiver
func NewUnarchiver(syncConfig *Sync, forceOverride bool, log log.Logger) *Unarchiver {
	return &Unarchiver{
		syncConfig:    syncConfig,
		forceOverride: forceOverride,
		log:           log,
	}
}

// Untar untars the given reader into the destination directory
func (u *Unarchiver) Untar(fromReader io.Reader, toPath string) error {
	fileCounter := 0
	gzr, err := gzip.NewReader(fromReader)
	if err != nil {
		return errors.Errorf("error decompressing: %v", err)
	}

	defer gzr.Close()

	tarReader := tar.NewReader(gzr)
	for {
		shouldContinue, err := u.untarNext(toPath, tarReader)
		if err != nil {
			return errors.Wrapf(err, "decompress %s", toPath)
		} else if shouldContinue == false {
			return nil
		}

		fileCounter++
		if fileCounter%500 == 0 {
			u.log.Infof("Downstream - Untared %d files...", fileCounter)
		}
	}
}

func (u *Unarchiver) untarNext(destPath string, tarReader *tar.Reader) (bool, error) {
	u.syncConfig.fileIndex.fileMapMutex.Lock()
	defer u.syncConfig.fileIndex.fileMapMutex.Unlock()

	header, err := tarReader.Next()
	if err != nil {
		if err != io.EOF {
			return false, errors.Wrap(err, "tar next")
		}

		return false, nil
	}

	relativePath := getRelativeFromFullPath("/"+header.Name, "")
	outFileName := path.Join(destPath, relativePath)
	baseName := path.Dir(outFileName)

	// Check if newer file is there and then don't override?
	stat, err := os.Stat(outFileName)
	if err == nil && u.forceOverride == false {
		if stat.ModTime().Unix() > header.FileInfo().ModTime().Unix() {
			// Update filemap otherwise we download and download again
			u.syncConfig.fileIndex.fileMap[relativePath] = &FileInformation{
				Name:        relativePath,
				Mtime:       stat.ModTime().Unix(),
				Size:        stat.Size(),
				IsDirectory: stat.IsDir(),
			}

			if stat.IsDir() == false {
				u.syncConfig.log.Infof("Downstream - Don't override %s because file has newer mTime timestamp", relativePath)
			}
			return true, nil
		}
	}

	if err := u.createAllFolders(baseName, 0755); err != nil {
		return false, err
	}

	if header.FileInfo().IsDir() {
		if err := u.createAllFolders(outFileName, 0755); err != nil {
			return false, err
		}

		u.syncConfig.fileIndex.CreateDirInFileMap(relativePath)
		return true, nil
	}

	// Create base dir in file map if it not already exists
	u.syncConfig.fileIndex.CreateDirInFileMap(getRelativeFromFullPath(baseName, destPath))

	// Create / Override file
	outFile, err := os.Create(outFileName)
	if err != nil {
		// Try again after 5 seconds
		time.Sleep(time.Second * 5)
		outFile, err = os.Create(outFileName)
		if err != nil {
			return false, errors.Wrap(err, "create file")
		}
	}

	defer outFile.Close()
	if _, err := io.Copy(outFile, tarReader); err != nil {
		return false, errors.Wrap(err, "copy file to reader")
	}

	if err := outFile.Close(); err != nil {
		return false, errors.Wrap(err, "close file")
	}

	if stat != nil {
		// Set old permissions correctly
		_ = os.Chmod(outFileName, stat.Mode())

		// Set owner & group correctly
		// TODO: Enable this on supported platforms
		// _ = os.Chown(outFileName, stat.Sys().(*syscall.Stat).Uid, stat.Sys().(*syscall.Stat_t).Gid)
	} else {
		// Set permissions
		_ = os.Chmod(outFileName, header.FileInfo().Mode())
	}

	// Set mod time correctly
	_ = os.Chtimes(outFileName, time.Now(), header.ModTime)

	// Execute command if defined
	if u.syncConfig.Options.FileChangeCmd != "" {
		cmdArgs := make([]string, 0, len(u.syncConfig.Options.FileChangeArgs))
		for _, arg := range u.syncConfig.Options.FileChangeArgs {
			if arg == "{}" {
				cmdArgs = append(cmdArgs, outFileName)
			} else {
				cmdArgs = append(cmdArgs, arg)
			}
		}

		out, err := exec.Command(u.syncConfig.Options.FileChangeCmd, cmdArgs...).CombinedOutput()
		if err != nil {
			return false, errors.Errorf("Error executing command '%s %s': %s => %v", u.syncConfig.Options.FileChangeCmd, strings.Join(cmdArgs, " "), string(out), err)
		}
	}

	// Update fileMap so that upstream does not upload the file
	u.syncConfig.fileIndex.fileMap[relativePath] = &FileInformation{
		Name:        relativePath,
		Mtime:       header.ModTime.Unix(),
		Size:        header.FileInfo().Size(),
		IsDirectory: false,
	}

	return true, nil
}

func (u *Unarchiver) createAllFolders(name string, perm os.FileMode) error {
	absPath, err := filepath.Abs(name)
	if err != nil {
		return err
	}

	slashPath := filepath.ToSlash(absPath)
	pathParts := strings.Split(slashPath, "/")
	for i := 1; i < len(pathParts); i++ {
		dirToCreate := strings.Join(pathParts[:i+1], "/")
		err := os.Mkdir(dirToCreate, perm)
		if err != nil {
			if os.IsExist(err) {
				continue
			}

			return errors.Errorf("Error creating %s: %v", dirToCreate, err)
		}

		if u.syncConfig.Options.DirCreateCmd != "" {
			cmdArgs := make([]string, 0, len(u.syncConfig.Options.DirCreateArgs))
			for _, arg := range u.syncConfig.Options.DirCreateArgs {
				if arg == "{}" {
					cmdArgs = append(cmdArgs, dirToCreate)
				} else {
					cmdArgs = append(cmdArgs, arg)
				}
			}

			out, err := exec.Command(u.syncConfig.Options.DirCreateCmd, cmdArgs...).CombinedOutput()
			if err != nil {
				return errors.Errorf("Error executing command '%s %s': %s => %v", u.syncConfig.Options.DirCreateCmd, strings.Join(cmdArgs, " "), string(out), err)
			}
		}
	}

	return nil
}

// Archiver is responsible for compressing specific files and folders within a target directory
type Archiver struct {
	basePath      string
	ignoreMatcher gitignore.IgnoreParser
	writer        *tar.Writer
	writtenFiles  map[string]*FileInformation
}

// NewArchiver creates a new archiver
func NewArchiver(basePath string, writer *tar.Writer, ignoreMatcher gitignore.IgnoreParser) *Archiver {
	return &Archiver{
		basePath: basePath,

		ignoreMatcher: ignoreMatcher,
		writer:        writer,
		writtenFiles:  make(map[string]*FileInformation),
	}
}

// WrittenFiles returns the written files by the archiver
func (a *Archiver) WrittenFiles() map[string]*FileInformation {
	return a.writtenFiles
}

// AddToArchive adds a new path to the archive
func (a *Archiver) AddToArchive(relativePath string) error {
	absFilepath := path.Join(a.basePath, relativePath)
	if a.writtenFiles[relativePath] != nil {
		return nil
	}

	// We skip files that are suddenly not there anymore
	stat, err := os.Stat(absFilepath)
	if err != nil {
		// config.Logf("[Upstream] Couldn't stat file %s: %s\n", absFilepath, err.Error())
		return nil
	}

	// Exclude files on the exclude list
	if a.ignoreMatcher != nil && util.MatchesPath(a.ignoreMatcher, relativePath, stat.IsDir()) {
		return nil
	}

	fileInformation := createFileInformationFromStat(relativePath, stat)
	if stat.IsDir() {
		// Recursively tar folder
		return a.tarFolder(fileInformation, stat)
	}

	return a.tarFile(fileInformation, stat)
}

func (a *Archiver) tarFolder(target *FileInformation, targetStat os.FileInfo) error {
	filepath := path.Join(a.basePath, target.Name)
	files, err := ioutil.ReadDir(filepath)
	if err != nil {
		// config.Logf("[Upstream] Couldn't read dir %s: %s\n", filepath, err.Error())
		return nil
	}

	if len(files) == 0 && target.Name != "" {
		// Case empty directory
		hdr, _ := tar.FileInfoHeader(targetStat, filepath)
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Name = target.Name
		if err := a.writer.WriteHeader(hdr); err != nil {
			return errors.Wrap(err, "tar write header")
		}

		a.writtenFiles[target.Name] = target
	}

	for _, f := range files {
		if err := a.AddToArchive(path.Join(target.Name, f.Name())); err != nil {
			return errors.Wrap(err, "recursive tar "+f.Name())
		}
	}

	return nil
}

func (a *Archiver) tarFile(target *FileInformation, targetStat os.FileInfo) error {
	var err error
	filepath := path.Join(a.basePath, target.Name)
	if targetStat.Mode()&os.ModeSymlink == os.ModeSymlink {
		if filepath, err = os.Readlink(filepath); err != nil {
			return nil
		}
	}

	// Case regular file
	f, err := os.Open(filepath)
	if err != nil {
		// We ignore open file and just treat it as okay
		return nil
	}

	defer f.Close()

	hdr, err := tar.FileInfoHeader(targetStat, filepath)
	if err != nil {
		return errors.Wrap(err, "create tar file info header")
	}
	hdr.Name = target.Name
	hdr.Uid = 0
	hdr.Gid = 0
	hdr.ModTime = time.Unix(target.Mtime, 0)

	if err := a.writer.WriteHeader(hdr); err != nil {
		return errors.Wrap(err, "tar write header")
	}

	// nothing more to do for non-regular
	if !targetStat.Mode().IsRegular() {
		return nil
	}

	if copied, err := io.CopyN(a.writer, f, targetStat.Size()); err != nil {
		return errors.Wrap(err, "tar copy file")
	} else if copied != targetStat.Size() {
		return errors.New("tar: file truncated during read")
	}

	a.writtenFiles[target.Name] = target
	return nil
}

func createFileInformationFromStat(relativePath string, stat os.FileInfo) *FileInformation {
	return &FileInformation{
		Name:        relativePath,
		Size:        stat.Size(),
		Mtime:       stat.ModTime().Unix(),
		MtimeNano:   stat.ModTime().UnixNano(),
		IsDirectory: stat.IsDir(),
	}
}
