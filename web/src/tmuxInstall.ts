export interface TmuxInstallGuide {
  systemLabel: string;
  command: string;
  supported: boolean;
  notice?: string;
}

const linuxGuides: Record<string, Omit<TmuxInstallGuide, "supported">> = {
  ubuntu: {
    systemLabel: "Ubuntu",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  debian: {
    systemLabel: "Debian",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  linuxmint: {
    systemLabel: "Linux Mint",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  pop: {
    systemLabel: "Pop!_OS",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  elementary: {
    systemLabel: "elementary OS",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  kali: {
    systemLabel: "Kali Linux",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  raspbian: {
    systemLabel: "Raspberry Pi OS",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  proxmox: {
    systemLabel: "Proxmox VE",
    command: "sudo apt-get update && sudo apt-get install -y tmux",
  },
  rhel: {
    systemLabel: "Red Hat Enterprise Linux",
    command: "sudo dnf install -y tmux",
  },
  rocky: {
    systemLabel: "Rocky Linux",
    command: "sudo dnf install -y tmux",
  },
  almalinux: {
    systemLabel: "AlmaLinux",
    command: "sudo dnf install -y tmux",
  },
  fedora: {
    systemLabel: "Fedora",
    command: "sudo dnf install -y tmux",
  },
  oracle: {
    systemLabel: "Oracle Linux",
    command: "sudo dnf install -y tmux",
  },
  centos: {
    systemLabel: "CentOS",
    command: "sudo yum install -y tmux",
  },
  amazon: {
    systemLabel: "Amazon Linux",
    command: "sudo yum install -y tmux",
  },
  arch: {
    systemLabel: "Arch Linux",
    command: "sudo pacman -S --needed tmux",
  },
  manjaro: {
    systemLabel: "Manjaro",
    command: "sudo pacman -S --needed tmux",
  },
  endeavouros: {
    systemLabel: "EndeavourOS",
    command: "sudo pacman -S --needed tmux",
  },
  alpine: {
    systemLabel: "Alpine Linux",
    command: "sudo apk add tmux",
  },
  opensuse: {
    systemLabel: "openSUSE",
    command: "sudo zypper install -y tmux",
  },
  gentoo: {
    systemLabel: "Gentoo",
    command: "sudo emerge app-misc/tmux",
  },
  nixos: {
    systemLabel: "NixOS",
    command: "nix-env -iA nixpkgs.tmux",
  },
  void: {
    systemLabel: "Void Linux",
    command: "sudo xbps-install -Sy tmux",
  },
};

const genericLinuxCommand = `if command -v apt-get >/dev/null; then
  sudo apt-get update && sudo apt-get install -y tmux
elif command -v dnf >/dev/null; then
  sudo dnf install -y tmux
elif command -v yum >/dev/null; then
  sudo yum install -y tmux
elif command -v apk >/dev/null; then
  sudo apk add tmux
elif command -v pacman >/dev/null; then
  sudo pacman -S --needed tmux
elif command -v zypper >/dev/null; then
  sudo zypper install -y tmux
else
  echo "请使用当前系统的软件包管理器安装 tmux"
fi`;

export function tmuxInstallGuide(
  platform = "",
  distribution = "",
): TmuxInstallGuide {
  const normalizedPlatform = platform.trim().toLowerCase();
  const normalizedDistribution = distribution.trim().toLowerCase();
  const linuxGuide = linuxGuides[normalizedDistribution];
  if (linuxGuide) return { ...linuxGuide, supported: true };
  if (normalizedPlatform === "windows") {
    return {
      systemLabel: "Windows",
      command: "",
      supported: false,
      notice:
        "原生 Windows SSH 环境无法运行 tmux，可使用普通模式直接打开 PowerShell 或 CMD。",
    };
  }
  if (normalizedPlatform === "macos") {
    return {
      systemLabel: "macOS",
      command: "brew install tmux",
      supported: true,
      notice: "需要先安装 Homebrew。",
    };
  }
  if (normalizedPlatform === "bsd") {
    return {
      systemLabel: "BSD",
      command: "sudo pkg install tmux",
      supported: true,
      notice: "该命令适用于 FreeBSD；其他 BSD 请使用对应的软件包管理器。",
    };
  }
  return {
    systemLabel: normalizedDistribution || "Linux",
    command: genericLinuxCommand,
    supported: true,
    notice: normalizedDistribution
      ? "未识别该发行版的软件包管理器，将按可用命令自动选择。"
      : "未识别发行版，将按可用的软件包管理器自动选择。",
  };
}
