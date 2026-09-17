package cmd

import (
	"fmt"
	"os"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

// Images a pull request body can actually show. gh has no upload endpoint and
// raw.githubusercontent.com answers 404 to a browser on a private repo, so the
// images go on a branch of their own and are linked through the blob viewer.
var assetsCmd = &cobra.Command{
	Use:   "assets",
	Short: "Images a PR/MR body can show: push them to the repo's assets branch",
	Long: `Screenshots for a pull request need a link the reviewer's browser can fetch.
On a private repository raw.githubusercontent.com cannot be, gh cannot upload,
and an image committed on the PR branch dies with the branch after the merge.

corgi assets push commits the images to docs/pr-assets/<key>/ on the branch
pr-assets/<key> (created the first time, appended to after, never merged),
pushes it, confirms origin has it, and prints the markdown to paste:
GitHub as .../blob/pr-assets/<key>/...?raw=true, GitLab as .../-/raw/....`,
}

var assetsPushCmd = &cobra.Command{
	Use:   "push <file>...",
	Short: "Commit images to pr-assets/<key>, push, print the markdown to paste",
	Long: `Commits the image files to docs/pr-assets/<key>/ on the branch pr-assets/<key>
of the repository in --dir (default: the current directory), pushes it, and prints
one markdown image per file plus a one-row table. The user's checkout is not
touched: the work happens in a throwaway worktree.

  corgi assets push shots/*.png --key ABC-123
  corgi assets push before.png after.png --key ABC-123 --dir ../api --json`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, files []string) {
		key, _ := cmd.Flags().GetString("key")
		dir, _ := cmd.Flags().GetString("dir")
		base, _ := cmd.Flags().GetString("base")
		res, err := utils.PushAssets(dir, key, base, files)
		if err != nil {
			exitWithError("assets_push", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(res)
			return
		}
		fmt.Fprintf(os.Stderr, "pushed %d file(s) to origin/%s at %s\n", len(res.Files), res.Branch, res.Head[:7])
		for _, f := range res.Files {
			fmt.Println(f.Markdown)
		}
		if len(res.Files) > 1 {
			fmt.Println()
			fmt.Print(res.Markdown)
		}
	},
}

func init() {
	assetsPushCmd.Flags().String("key", "", "story key that names the assets branch pr-assets/<key> (required)")
	assetsPushCmd.Flags().String("dir", ".", "repository directory")
	assetsPushCmd.Flags().String("base", "", "branch the assets branch starts from the first time (default: origin's default branch)")
	_ = assetsPushCmd.MarkFlagRequired("key")
	assetsCmd.AddCommand(assetsPushCmd)
	rootCmd.AddCommand(assetsCmd)
}
