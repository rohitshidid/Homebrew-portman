class Portmap < Formula
  desc "Show listening ports with owning app, Docker container, and service labels"
  homepage "https://github.com/rohitshidid/Homebrew-portman"
  url "https://github.com/rohitshidid/Homebrew-portman.git",
    tag: "v0.2.0"
  license "MIT"
  head "https://github.com/rohitshidid/Homebrew-portman.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/portmap"
  end

  test do
    assert_match "portmap #{version}", shell_output("#{bin}/portmap --version")

    output = shell_output("#{bin}/portmap --no-docker")
    assert_match "PORT", output
    assert_match "SERVICE", output
  end
end
