class Llamawizard < Formula
  desc "Terminal wizard for setting up a local LLM stack on macOS"
  homepage "https://github.com/eduard-lt/llamawizard"
  license "MIT"
  head "https://github.com/eduard-lt/llamawizard.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", "-o", bin/"llamawizard", "./cmd/llamawizard/"
  end

  test do
    system "#{bin}/llamawizard", "help"
  end
end
