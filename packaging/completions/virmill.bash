# bash completion for virmill                              -*- shell-script -*-

__virmill_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE:-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}

# Homebrew on Macs have version 1.3 of bash-completion which doesn't include
# _init_completion. This is a very minimal version of that function.
__virmill_init_completion()
{
    COMPREPLY=()
    _get_comp_words_by_ref "$@" cur prev words cword
}

__virmill_index_of_word()
{
    local w word=$1
    shift
    index=0
    for w in "$@"; do
        [[ $w = "$word" ]] && return
        index=$((index+1))
    done
    index=-1
}

__virmill_contains_word()
{
    local w word=$1; shift
    for w in "$@"; do
        [[ $w = "$word" ]] && return
    done
    return 1
}

__virmill_handle_go_custom_completion()
{
    __virmill_debug "${FUNCNAME[0]}: cur is ${cur}, words[*] is ${words[*]}, #words[@] is ${#words[@]}"

    local shellCompDirectiveError=1
    local shellCompDirectiveNoSpace=2
    local shellCompDirectiveNoFileComp=4
    local shellCompDirectiveFilterFileExt=8
    local shellCompDirectiveFilterDirs=16

    local out requestComp lastParam lastChar comp directive args

    # Prepare the command to request completions for the program.
    # Calling ${words[0]} instead of directly virmill allows handling aliases
    args=("${words[@]:1}")
    # Disable ActiveHelp which is not supported for bash completion v1
    requestComp="VIRMILL_ACTIVE_HELP=0 ${words[0]} __completeNoDesc ${args[*]}"

    lastParam=${words[$((${#words[@]}-1))]}
    lastChar=${lastParam:$((${#lastParam}-1)):1}
    __virmill_debug "${FUNCNAME[0]}: lastParam ${lastParam}, lastChar ${lastChar}"

    if [ -z "${cur}" ] && [ "${lastChar}" != "=" ]; then
        # If the last parameter is complete (there is a space following it)
        # We add an extra empty parameter so we can indicate this to the go method.
        __virmill_debug "${FUNCNAME[0]}: Adding extra empty parameter"
        requestComp="${requestComp} \"\""
    fi

    __virmill_debug "${FUNCNAME[0]}: calling ${requestComp}"
    # Use eval to handle any environment variables and such
    out=$(eval "${requestComp}" 2>/dev/null)

    # Extract the directive integer at the very end of the output following a colon (:)
    directive=${out##*:}
    # Remove the directive
    out=${out%:*}
    if [ "${directive}" = "${out}" ]; then
        # There is not directive specified
        directive=0
    fi
    __virmill_debug "${FUNCNAME[0]}: the completion directive is: ${directive}"
    __virmill_debug "${FUNCNAME[0]}: the completions are: ${out}"

    if [ $((directive & shellCompDirectiveError)) -ne 0 ]; then
        # Error code.  No completion.
        __virmill_debug "${FUNCNAME[0]}: received error from custom completion go code"
        return
    else
        if [ $((directive & shellCompDirectiveNoSpace)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __virmill_debug "${FUNCNAME[0]}: activating no space"
                compopt -o nospace
            fi
        fi
        if [ $((directive & shellCompDirectiveNoFileComp)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __virmill_debug "${FUNCNAME[0]}: activating no file completion"
                compopt +o default
            fi
        fi
    fi

    if [ $((directive & shellCompDirectiveFilterFileExt)) -ne 0 ]; then
        # File extension filtering
        local fullFilter filter filteringCmd
        # Do not use quotes around the $out variable or else newline
        # characters will be kept.
        for filter in ${out}; do
            fullFilter+="$filter|"
        done

        filteringCmd="_filedir $fullFilter"
        __virmill_debug "File filtering command: $filteringCmd"
        $filteringCmd
    elif [ $((directive & shellCompDirectiveFilterDirs)) -ne 0 ]; then
        # File completion for directories only
        local subdir
        # Use printf to strip any trailing newline
        subdir=$(printf "%s" "${out}")
        if [ -n "$subdir" ]; then
            __virmill_debug "Listing directories in $subdir"
            __virmill_handle_subdirs_in_dir_flag "$subdir"
        else
            __virmill_debug "Listing directories in ."
            _filedir -d
        fi
    else
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${out}" -- "$cur")
    fi
}

__virmill_handle_reply()
{
    __virmill_debug "${FUNCNAME[0]}"
    local comp
    case $cur in
        -*)
            if [[ $(type -t compopt) = "builtin" ]]; then
                compopt -o nospace
            fi
            local allflags
            if [ ${#must_have_one_flag[@]} -ne 0 ]; then
                allflags=("${must_have_one_flag[@]}")
            else
                allflags=("${flags[*]} ${two_word_flags[*]}")
            fi
            while IFS='' read -r comp; do
                COMPREPLY+=("$comp")
            done < <(compgen -W "${allflags[*]}" -- "$cur")
            if [[ $(type -t compopt) = "builtin" ]]; then
                [[ "${COMPREPLY[0]}" == *= ]] || compopt +o nospace
            fi

            # complete after --flag=abc
            if [[ $cur == *=* ]]; then
                if [[ $(type -t compopt) = "builtin" ]]; then
                    compopt +o nospace
                fi

                local index flag
                flag="${cur%=*}"
                __virmill_index_of_word "${flag}" "${flags_with_completion[@]}"
                COMPREPLY=()
                if [[ ${index} -ge 0 ]]; then
                    PREFIX=""
                    cur="${cur#*=}"
                    ${flags_completion[${index}]}
                    if [ -n "${ZSH_VERSION:-}" ]; then
                        # zsh completion needs --flag= prefix
                        eval "COMPREPLY=( \"\${COMPREPLY[@]/#/${flag}=}\" )"
                    fi
                fi
            fi

            if [[ -z "${flag_parsing_disabled}" ]]; then
                # If flag parsing is enabled, we have completed the flags and can return.
                # If flag parsing is disabled, we may not know all (or any) of the flags, so we fallthrough
                # to possibly call handle_go_custom_completion.
                return 0;
            fi
            ;;
    esac

    # check if we are handling a flag with special work handling
    local index
    __virmill_index_of_word "${prev}" "${flags_with_completion[@]}"
    if [[ ${index} -ge 0 ]]; then
        ${flags_completion[${index}]}
        return
    fi

    # we are parsing a flag and don't have a special handler, no completion
    if [[ ${cur} != "${words[cword]}" ]]; then
        return
    fi

    local completions
    completions=("${commands[@]}")
    if [[ ${#must_have_one_noun[@]} -ne 0 ]]; then
        completions+=("${must_have_one_noun[@]}")
    elif [[ -n "${has_completion_function}" ]]; then
        # if a go completion function is provided, defer to that function
        __virmill_handle_go_custom_completion
    fi
    if [[ ${#must_have_one_flag[@]} -ne 0 ]]; then
        completions+=("${must_have_one_flag[@]}")
    fi
    while IFS='' read -r comp; do
        COMPREPLY+=("$comp")
    done < <(compgen -W "${completions[*]}" -- "$cur")

    if [[ ${#COMPREPLY[@]} -eq 0 && ${#noun_aliases[@]} -gt 0 && ${#must_have_one_noun[@]} -ne 0 ]]; then
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${noun_aliases[*]}" -- "$cur")
    fi

    if [[ ${#COMPREPLY[@]} -eq 0 ]]; then
        if declare -F __virmill_custom_func >/dev/null; then
            # try command name qualified custom func
            __virmill_custom_func
        else
            # otherwise fall back to unqualified for compatibility
            declare -F __custom_func >/dev/null && __custom_func
        fi
    fi

    # available in bash-completion >= 2, not always present on macOS
    if declare -F __ltrim_colon_completions >/dev/null; then
        __ltrim_colon_completions "$cur"
    fi

    # If there is only 1 completion and it is a flag with an = it will be completed
    # but we don't want a space after the =
    if [[ "${#COMPREPLY[@]}" -eq "1" ]] && [[ $(type -t compopt) = "builtin" ]] && [[ "${COMPREPLY[0]}" == --*= ]]; then
       compopt -o nospace
    fi
}

# The arguments should be in the form "ext1|ext2|extn"
__virmill_handle_filename_extension_flag()
{
    local ext="$1"
    _filedir "@(${ext})"
}

__virmill_handle_subdirs_in_dir_flag()
{
    local dir="$1"
    pushd "${dir}" >/dev/null 2>&1 && _filedir -d && popd >/dev/null 2>&1 || return
}

__virmill_handle_flag()
{
    __virmill_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    # if a command required a flag, and we found it, unset must_have_one_flag()
    local flagname=${words[c]}
    local flagvalue=""
    # if the word contained an =
    if [[ ${words[c]} == *"="* ]]; then
        flagvalue=${flagname#*=} # take in as flagvalue after the =
        flagname=${flagname%=*} # strip everything after the =
        flagname="${flagname}=" # but put the = back
    fi
    __virmill_debug "${FUNCNAME[0]}: looking for ${flagname}"
    if __virmill_contains_word "${flagname}" "${must_have_one_flag[@]}"; then
        must_have_one_flag=()
    fi

    # if you set a flag which only applies to this command, don't show subcommands
    if __virmill_contains_word "${flagname}" "${local_nonpersistent_flags[@]}"; then
      commands=()
    fi

    # keep flag value with flagname as flaghash
    # flaghash variable is an associative array which is only supported in bash > 3.
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        if [ -n "${flagvalue}" ] ; then
            flaghash[${flagname}]=${flagvalue}
        elif [ -n "${words[ $((c+1)) ]}" ] ; then
            flaghash[${flagname}]=${words[ $((c+1)) ]}
        else
            flaghash[${flagname}]="true" # pad "true" for bool flag
        fi
    fi

    # skip the argument to a two word flag
    if [[ ${words[c]} != *"="* ]] && __virmill_contains_word "${words[c]}" "${two_word_flags[@]}"; then
        __virmill_debug "${FUNCNAME[0]}: found a flag ${words[c]}, skip the next argument"
        c=$((c+1))
        # if we are looking for a flags value, don't show commands
        if [[ $c -eq $cword ]]; then
            commands=()
        fi
    fi

    c=$((c+1))

}

__virmill_handle_noun()
{
    __virmill_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    if __virmill_contains_word "${words[c]}" "${must_have_one_noun[@]}"; then
        must_have_one_noun=()
    elif __virmill_contains_word "${words[c]}" "${noun_aliases[@]}"; then
        must_have_one_noun=()
    fi

    nouns+=("${words[c]}")
    c=$((c+1))
}

__virmill_handle_command()
{
    __virmill_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    local next_command
    if [[ -n ${last_command} ]]; then
        next_command="_${last_command}_${words[c]//:/__}"
    else
        if [[ $c -eq 0 ]]; then
            next_command="_virmill_root_command"
        else
            next_command="_${words[c]//:/__}"
        fi
    fi
    c=$((c+1))
    __virmill_debug "${FUNCNAME[0]}: looking for ${next_command}"
    declare -F "$next_command" >/dev/null && $next_command
}

__virmill_handle_word()
{
    if [[ $c -ge $cword ]]; then
        __virmill_handle_reply
        return
    fi
    __virmill_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"
    if [[ "${words[c]}" == -* ]]; then
        __virmill_handle_flag
    elif __virmill_contains_word "${words[c]}" "${commands[@]}"; then
        __virmill_handle_command
    elif [[ $c -eq 0 ]]; then
        __virmill_handle_command
    elif __virmill_contains_word "${words[c]}" "${command_aliases[@]}"; then
        # aliashash variable is an associative array which is only supported in bash > 3.
        if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
            words[c]=${aliashash[${words[c]}]}
            __virmill_handle_command
        else
            __virmill_handle_noun
        fi
    else
        __virmill_handle_noun
    fi
    __virmill_handle_word
}

_virmill_backup_create()
{
    last_command="virmill_backup_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_policy_preview()
{
    last_command="virmill_backup_policy_preview"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_policy_validate()
{
    last_command="virmill_backup_policy_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_policy()
{
    last_command="virmill_backup_policy"

    command_aliases=()

    commands=()
    commands+=("preview")
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_receipt_export()
{
    last_command="virmill_backup_receipt_export"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_receipt_read()
{
    last_command="virmill_backup_receipt_read"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_receipt_show()
{
    last_command="virmill_backup_receipt_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_receipt()
{
    last_command="virmill_backup_receipt"

    command_aliases=()

    commands=()
    commands+=("export")
    commands+=("read")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_receipts()
{
    last_command="virmill_backup_receipts"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_repository_check()
{
    last_command="virmill_backup_repository_check"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_repository_init()
{
    last_command="virmill_backup_repository_init"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_repository()
{
    last_command="virmill_backup_repository"

    command_aliases=()

    commands=()
    commands+=("check")
    commands+=("init")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_restore()
{
    last_command="virmill_backup_restore"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_result()
{
    last_command="virmill_backup_result"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup_verify-manifest()
{
    last_command="virmill_backup_verify-manifest"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_backup()
{
    last_command="virmill_backup"

    command_aliases=()

    commands=()
    commands+=("create")
    commands+=("policy")
    commands+=("receipt")
    commands+=("receipts")
    commands+=("repository")
    commands+=("restore")
    commands+=("result")
    commands+=("verify-manifest")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_completion()
{
    last_command="virmill_completion"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--help")
    flags+=("-h")
    local_nonpersistent_flags+=("--help")
    local_nonpersistent_flags+=("-h")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    must_have_one_noun+=("bash")
    must_have_one_noun+=("fish")
    must_have_one_noun+=("powershell")
    must_have_one_noun+=("zsh")
    noun_aliases=()
}

_virmill_config_validate()
{
    last_command="virmill_config_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_config()
{
    last_command="virmill_config"

    command_aliases=()

    commands=()
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_device_usb_list()
{
    last_command="virmill_device_usb_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_device_usb()
{
    last_command="virmill_device_usb"

    command_aliases=()

    commands=()
    commands+=("list")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_device()
{
    last_command="virmill_device"

    command_aliases=()

    commands=()
    commands+=("usb")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_doctor()
{
    last_command="virmill_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_guest_recipe_result()
{
    last_command="virmill_guest_recipe_result"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_guest_recipe_run()
{
    last_command="virmill_guest_recipe_run"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_guest_recipe()
{
    last_command="virmill_guest_recipe"

    command_aliases=()

    commands=()
    commands+=("result")
    commands+=("run")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_guest_tools_catalog()
{
    last_command="virmill_guest_tools_catalog"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_guest_tools_install()
{
    last_command="virmill_guest_tools_install"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_guest_tools()
{
    last_command="virmill_guest_tools"

    command_aliases=()

    commands=()
    commands+=("catalog")
    commands+=("install")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_guest()
{
    last_command="virmill_guest"

    command_aliases=()

    commands=()
    commands+=("recipe")
    commands+=("tools")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_help()
{
    last_command="virmill_help"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    has_completion_function=1
    noun_aliases=()
}

_virmill_host_capabilities()
{
    last_command="virmill_host_capabilities"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_host_helper_identity()
{
    last_command="virmill_host_helper_identity"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_host_helper()
{
    last_command="virmill_host_helper"

    command_aliases=()

    commands=()
    commands+=("identity")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_host_inspect()
{
    last_command="virmill_host_inspect"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_host_pci_list()
{
    last_command="virmill_host_pci_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_host_pci()
{
    last_command="virmill_host_pci"

    command_aliases=()

    commands=()
    commands+=("list")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_host()
{
    last_command="virmill_host"

    command_aliases=()

    commands=()
    commands+=("capabilities")
    commands+=("helper")
    commands+=("inspect")
    commands+=("pci")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_describe()
{
    last_command="virmill_import_describe"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_discard()
{
    last_command="virmill_import_discard"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--keep-images")
    local_nonpersistent_flags+=("--keep-images")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_inspect()
{
    last_command="virmill_import_inspect"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_prepare()
{
    last_command="virmill_import_prepare"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_prepare-disks()
{
    last_command="virmill_import_prepare-disks"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_prepare-install()
{
    last_command="virmill_import_prepare-install"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_result()
{
    last_command="virmill_import_result"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_source_describe()
{
    last_command="virmill_import_source_describe"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_source()
{
    last_command="virmill_import_source"

    command_aliases=()

    commands=()
    commands+=("describe")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_sources()
{
    last_command="virmill_import_sources"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import_verify()
{
    last_command="virmill_import_verify"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_import()
{
    last_command="virmill_import"

    command_aliases=()

    commands=()
    commands+=("describe")
    commands+=("discard")
    commands+=("inspect")
    commands+=("prepare")
    commands+=("prepare-disks")
    commands+=("prepare-install")
    commands+=("result")
    commands+=("source")
    commands+=("sources")
    commands+=("verify")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_lab_validate()
{
    last_command="virmill_lab_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_lab()
{
    last_command="virmill_lab"

    command_aliases=()

    commands=()
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_cidr_check()
{
    last_command="virmill_network_cidr_check"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_cidr()
{
    last_command="virmill_network_cidr"

    command_aliases=()

    commands=()
    commands+=("check")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_create()
{
    last_command="virmill_network_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_creation_result()
{
    last_command="virmill_network_creation_result"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_creation_resume()
{
    last_command="virmill_network_creation_resume"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_creation()
{
    last_command="virmill_network_creation"

    command_aliases=()

    commands=()
    commands+=("result")
    commands+=("resume")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_list()
{
    last_command="virmill_network_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network_show()
{
    last_command="virmill_network_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_network()
{
    last_command="virmill_network"

    command_aliases=()

    commands=()
    commands+=("cidr")
    commands+=("create")
    commands+=("creation")
    commands+=("list")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_operation_cancel()
{
    last_command="virmill_operation_cancel"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_operation_dispose-disk-addition()
{
    last_command="virmill_operation_dispose-disk-addition"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_operation_list()
{
    last_command="virmill_operation_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_operation_reconcile()
{
    last_command="virmill_operation_reconcile"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_operation_show()
{
    last_command="virmill_operation_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_operation_watch()
{
    last_command="virmill_operation_watch"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--follow")
    local_nonpersistent_flags+=("--follow")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_operation()
{
    last_command="virmill_operation"

    command_aliases=()

    commands=()
    commands+=("cancel")
    commands+=("dispose-disk-addition")
    commands+=("list")
    commands+=("reconcile")
    commands+=("show")
    commands+=("watch")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plan_apply()
{
    last_command="virmill_plan_apply"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--ack=")
    two_word_flags+=("--ack")
    local_nonpersistent_flags+=("--ack")
    local_nonpersistent_flags+=("--ack=")
    flags+=("--detach")
    local_nonpersistent_flags+=("--detach")
    flags+=("--digest=")
    two_word_flags+=("--digest")
    local_nonpersistent_flags+=("--digest")
    local_nonpersistent_flags+=("--digest=")
    flags+=("--idempotency-key=")
    two_word_flags+=("--idempotency-key")
    local_nonpersistent_flags+=("--idempotency-key")
    local_nonpersistent_flags+=("--idempotency-key=")
    flags+=("--wait")
    local_nonpersistent_flags+=("--wait")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plan_show()
{
    last_command="virmill_plan_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plan()
{
    last_command="virmill_plan"

    command_aliases=()

    commands=()
    commands+=("apply")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_call()
{
    last_command="virmill_plugin_call"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_disable()
{
    last_command="virmill_plugin_disable"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_enable()
{
    last_command="virmill_plugin_enable"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_install()
{
    last_command="virmill_plugin_install"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--key-id=")
    two_word_flags+=("--key-id")
    local_nonpersistent_flags+=("--key-id")
    local_nonpersistent_flags+=("--key-id=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--public-key=")
    two_word_flags+=("--public-key")
    local_nonpersistent_flags+=("--public-key")
    local_nonpersistent_flags+=("--public-key=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_list()
{
    last_command="virmill_plugin_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_new()
{
    last_command="virmill_plugin_new"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--language=")
    two_word_flags+=("--language")
    local_nonpersistent_flags+=("--language")
    local_nonpersistent_flags+=("--language=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--sdk-directory=")
    two_word_flags+=("--sdk-directory")
    local_nonpersistent_flags+=("--sdk-directory")
    local_nonpersistent_flags+=("--sdk-directory=")
    flags+=("--type=")
    two_word_flags+=("--type")
    local_nonpersistent_flags+=("--type")
    local_nonpersistent_flags+=("--type=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_pack()
{
    last_command="virmill_plugin_pack"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--destination=")
    two_word_flags+=("--destination")
    local_nonpersistent_flags+=("--destination")
    local_nonpersistent_flags+=("--destination=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--key-id=")
    two_word_flags+=("--key-id")
    local_nonpersistent_flags+=("--key-id")
    local_nonpersistent_flags+=("--key-id=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--signing-key-file=")
    two_word_flags+=("--signing-key-file")
    local_nonpersistent_flags+=("--signing-key-file")
    local_nonpersistent_flags+=("--signing-key-file=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_permissions_grant()
{
    last_command="virmill_plugin_permissions_grant"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_permissions_revoke()
{
    last_command="virmill_plugin_permissions_revoke"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_permissions_show()
{
    last_command="virmill_plugin_permissions_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_permissions()
{
    last_command="virmill_plugin_permissions"

    command_aliases=()

    commands=()
    commands+=("grant")
    commands+=("revoke")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_remove()
{
    last_command="virmill_plugin_remove"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_result()
{
    last_command="virmill_plugin_result"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_rollback()
{
    last_command="virmill_plugin_rollback"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_show()
{
    last_command="virmill_plugin_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_test()
{
    last_command="virmill_plugin_test"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_update()
{
    last_command="virmill_plugin_update"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--key-id=")
    two_word_flags+=("--key-id")
    local_nonpersistent_flags+=("--key-id")
    local_nonpersistent_flags+=("--key-id=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--public-key=")
    two_word_flags+=("--public-key")
    local_nonpersistent_flags+=("--public-key")
    local_nonpersistent_flags+=("--public-key=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin_validate()
{
    last_command="virmill_plugin_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_plugin()
{
    last_command="virmill_plugin"

    command_aliases=()

    commands=()
    commands+=("call")
    commands+=("disable")
    commands+=("enable")
    commands+=("install")
    commands+=("list")
    commands+=("new")
    commands+=("pack")
    commands+=("permissions")
    commands+=("remove")
    commands+=("result")
    commands+=("rollback")
    commands+=("show")
    commands+=("test")
    commands+=("update")
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_snapshot_create()
{
    last_command="virmill_snapshot_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_snapshot_list()
{
    last_command="virmill_snapshot_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_snapshot_restore()
{
    last_command="virmill_snapshot_restore"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_snapshot_show()
{
    last_command="virmill_snapshot_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_snapshot()
{
    last_command="virmill_snapshot"

    command_aliases=()

    commands=()
    commands+=("create")
    commands+=("list")
    commands+=("restore")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_access_grant()
{
    last_command="virmill_storage_access_grant"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_access_result()
{
    last_command="virmill_storage_access_result"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_access_revoke()
{
    last_command="virmill_storage_access_revoke"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_access()
{
    last_command="virmill_storage_access"

    command_aliases=()

    commands=()
    commands+=("grant")
    commands+=("result")
    commands+=("revoke")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_pool_create()
{
    last_command="virmill_storage_pool_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--no-autostart")
    local_nonpersistent_flags+=("--no-autostart")
    flags+=("--path=")
    two_word_flags+=("--path")
    local_nonpersistent_flags+=("--path")
    local_nonpersistent_flags+=("--path=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_pool_list()
{
    last_command="virmill_storage_pool_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_pool_show()
{
    last_command="virmill_storage_pool_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_pool_start()
{
    last_command="virmill_storage_pool_start"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--no-autostart")
    local_nonpersistent_flags+=("--no-autostart")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage_pool()
{
    last_command="virmill_storage_pool"

    command_aliases=()

    commands=()
    commands+=("create")
    commands+=("list")
    commands+=("show")
    commands+=("start")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_storage()
{
    last_command="virmill_storage"

    command_aliases=()

    commands=()
    commands+=("access")
    commands+=("pool")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_tui()
{
    last_command="virmill_tui"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_update_check()
{
    last_command="virmill_update_check"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_update_checks()
{
    last_command="virmill_update_checks"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    must_have_one_noun+=("off")
    must_have_one_noun+=("on")
    noun_aliases=()
}

_virmill_update()
{
    last_command="virmill_update"

    command_aliases=()

    commands=()
    commands+=("check")
    commands+=("checks")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--download-only")
    local_nonpersistent_flags+=("--download-only")
    flags+=("--yes")
    local_nonpersistent_flags+=("--yes")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_version()
{
    last_command="virmill_version"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_autostart()
{
    last_command="virmill_vm_autostart"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_boot_set()
{
    last_command="virmill_vm_boot_set"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_boot_show()
{
    last_command="virmill_vm_boot_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_boot()
{
    last_command="virmill_vm_boot"

    command_aliases=()

    commands=()
    commands+=("set")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_clone()
{
    last_command="virmill_vm_clone"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_console_open()
{
    last_command="virmill_vm_console_open"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--choice=")
    two_word_flags+=("--choice")
    local_nonpersistent_flags+=("--choice")
    local_nonpersistent_flags+=("--choice=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_console_show()
{
    last_command="virmill_vm_console_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_console()
{
    last_command="virmill_vm_console"

    command_aliases=()

    commands=()
    commands+=("open")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_create()
{
    last_command="virmill_vm_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_creation_accept()
{
    last_command="virmill_vm_creation_accept"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_creation_cleanup()
{
    last_command="virmill_vm_creation_cleanup"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_creation_options()
{
    last_command="virmill_vm_creation_options"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_creation_result()
{
    last_command="virmill_vm_creation_result"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_creation_resume()
{
    last_command="virmill_vm_creation_resume"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_creation()
{
    last_command="virmill_vm_creation"

    command_aliases=()

    commands=()
    commands+=("accept")
    commands+=("cleanup")
    commands+=("options")
    commands+=("result")
    commands+=("resume")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_disk_add()
{
    last_command="virmill_vm_disk_add"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_disk_grow()
{
    last_command="virmill_vm_disk_grow"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_disk_move()
{
    last_command="virmill_vm_disk_move"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_disk()
{
    last_command="virmill_vm_disk"

    command_aliases=()

    commands=()
    commands+=("add")
    commands+=("grow")
    commands+=("move")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_guest-agent_enable()
{
    last_command="virmill_vm_guest-agent_enable"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_guest-agent_show()
{
    last_command="virmill_vm_guest-agent_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_guest-agent()
{
    last_command="virmill_vm_guest-agent"

    command_aliases=()

    commands=()
    commands+=("enable")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_list()
{
    last_command="virmill_vm_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_pause()
{
    last_command="virmill_vm_pause"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_readiness_show()
{
    last_command="virmill_vm_readiness_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_readiness()
{
    last_command="virmill_vm_readiness"

    command_aliases=()

    commands=()
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_reboot()
{
    last_command="virmill_vm_reboot"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_recovery_auxiliary_inspect()
{
    last_command="virmill_vm_recovery_auxiliary_inspect"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_recovery_auxiliary()
{
    last_command="virmill_vm_recovery_auxiliary"

    command_aliases=()

    commands=()
    commands+=("inspect")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_recovery_inspect()
{
    last_command="virmill_vm_recovery_inspect"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_recovery()
{
    last_command="virmill_vm_recovery"

    command_aliases=()

    commands=()
    commands+=("auxiliary")
    commands+=("inspect")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_remove()
{
    last_command="virmill_vm_remove"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--delete-disk=")
    two_word_flags+=("--delete-disk")
    local_nonpersistent_flags+=("--delete-disk")
    local_nonpersistent_flags+=("--delete-disk=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_resources_show()
{
    last_command="virmill_vm_resources_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_resources()
{
    last_command="virmill_vm_resources"

    command_aliases=()

    commands=()
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_restore-saved()
{
    last_command="virmill_vm_restore-saved"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_resume()
{
    last_command="virmill_vm_resume"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_save()
{
    last_command="virmill_vm_save"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_set()
{
    last_command="virmill_vm_set"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_show()
{
    last_command="virmill_vm_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_start()
{
    last_command="virmill_vm_start"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm_stop()
{
    last_command="virmill_vm_stop"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--after=")
    two_word_flags+=("--after")
    local_nonpersistent_flags+=("--after")
    local_nonpersistent_flags+=("--after=")
    flags+=("--hard")
    local_nonpersistent_flags+=("--hard")
    flags+=("--input=")
    two_word_flags+=("--input")
    local_nonpersistent_flags+=("--input")
    local_nonpersistent_flags+=("--input=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_vm()
{
    last_command="virmill_vm"

    command_aliases=()

    commands=()
    commands+=("autostart")
    commands+=("boot")
    commands+=("clone")
    commands+=("console")
    commands+=("create")
    commands+=("creation")
    commands+=("disk")
    commands+=("guest-agent")
    commands+=("list")
    commands+=("pause")
    commands+=("readiness")
    commands+=("reboot")
    commands+=("recovery")
    commands+=("remove")
    commands+=("resources")
    commands+=("restore-saved")
    commands+=("resume")
    commands+=("save")
    commands+=("set")
    commands+=("show")
    commands+=("start")
    commands+=("stop")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_virmill_root_command()
{
    last_command="virmill"

    command_aliases=()

    commands=()
    commands+=("backup")
    commands+=("completion")
    commands+=("config")
    commands+=("device")
    commands+=("doctor")
    commands+=("guest")
    commands+=("help")
    commands+=("host")
    commands+=("import")
    commands+=("lab")
    commands+=("network")
    commands+=("operation")
    commands+=("plan")
    commands+=("plugin")
    commands+=("snapshot")
    commands+=("storage")
    commands+=("tui")
    commands+=("update")
    commands+=("version")
    commands+=("vm")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    flags+=("--connection=")
    two_word_flags+=("--connection")
    flags+=("--no-color")
    flags+=("--non-interactive")
    flags+=("--output=")
    two_word_flags+=("--output")
    flags+=("--quiet")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

__start_virmill()
{
    local cur prev words cword split
    declare -A flaghash 2>/dev/null || :
    declare -A aliashash 2>/dev/null || :
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion -s || return
    else
        __virmill_init_completion -n "=" || return
    fi

    local c=0
    local flag_parsing_disabled=
    local flags=()
    local two_word_flags=()
    local local_nonpersistent_flags=()
    local flags_with_completion=()
    local flags_completion=()
    local commands=("virmill")
    local command_aliases=()
    local must_have_one_flag=()
    local must_have_one_noun=()
    local has_completion_function=""
    local last_command=""
    local nouns=()
    local noun_aliases=()

    __virmill_handle_word
}

if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_virmill virmill
else
    complete -o default -o nospace -F __start_virmill virmill
fi

# ex: ts=4 sw=4 et filetype=sh
