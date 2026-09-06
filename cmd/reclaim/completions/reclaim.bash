# bash completion for reclaim
#
# Unit ids come from the binary rather than a list baked in here: which units
# exist depends on what is installed, and a hardcoded list would go stale on the
# first machine that differs.

_reclaim() {
    local cur prev cmds
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cmds="clean status analyze history version completion"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "$cmds" -- "$cur") )
        return
    fi

    case "$prev" in
        --only|--exclude)
            COMPREPLY=( $(compgen -W "$(reclaim __units 2>/dev/null)" -- "$cur") )
            return
            ;;
        --with)
            COMPREPLY=( $(compgen -W "gradle maven jetbrains browsers playwright docker docker-volumes claude-vm system claude-jobs claude-plugins claude-history heavy flatpak kernels models" -- "$cur") )
            return
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
            return
            ;;
        --free|--below|--tier|--workers|--sites-idle|--sites-root|--min|--older|-n)
            return
            ;;
    esac

    case "${COMP_WORDS[1]}" in
        clean)
            COMPREPLY=( $(compgen -W "--apply --yes --free --auto --below --tier --allow-lossy \
                --discover --json --workers --sites-idle --sites-root --only --exclude --with \
                --gradle --maven --jetbrains --browsers --playwright --docker --docker-volumes \
                --claude-vm --system --claude-jobs --claude-plugins --claude-history \
                --heavy --flatpak --kernels --models" -- "$cur") )
            ;;
        analyze)
            COMPREPLY=( $(compgen -W "--min -n --installers --older --json" -- "$cur") )
            ;;
        history)
            COMPREPLY=( $(compgen -W "-n" -- "$cur") )
            ;;
    esac
}

complete -F _reclaim reclaim
