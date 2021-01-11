export var init = null;

// Pane is a .pane element.
// It can either be
// .pane__root, containing single .pane child or nothing
// .pane__horizontal, containing at least two children and separators
// .pane__vertical, containing at least two children ans separators
// .pane__leaf, containing arbitrary contents
// (horizontal cannot directly contain horizontal, same for vertical)

// Custom events:
// - paneresize
// - paneremove

// Plan. What we need:
// - add a pane and fill it with given contents
// - remove pane
// - receive resize events
// - some controls to move panes around
// - save/restore pane configuration for a given project

var overlay = document.createElement("div");
overlay.id = "pane_separator_overlay";
overlay.classList.add("pane_separator_overlay");
document.body.appendChild(overlay);

function makeLeaf(element) {
    let leaf = document.createElement("div");
    leaf.classList.add("pane", "pane__leaf");
    leaf.appendChild(element);
    return leaf;
}

function makeSeparator() {
    let separator = document.createElement("div");
    separator.classList.add("pane--separator");
    separator.addEventListener("mousedown", dragStart);
    return separator;
}

var dragging = null;

function dragStart() {
    if (dragging != null) {
        dragStop();
        return;
    }
    var separator = this;
    var container = this.parentElement;
    var horizontal;
    if (container.classList.contains("pane__horizontal")) {
        horizontal = true;
    } else if (dragging.container.classList.contains("pane__vertical")) {
        horizontal = false;
    } else {
        throw new Error("invalid pane tree structure");
    }
    dragging = {
        separator: separator,
        container: container,
        horizontal: horizontal,
    };
    separator.classList.add("pane--separator__active");
    overlay.style.zIndex = +1;
    overlay.style.cursor = window.getComputedStyle(separator).cursor;
    window .addEventListener("mousemove",  drag);
    window .addEventListener("mouseup",    dragStop);
    overlay.addEventListener("mouseleave", dragStop);
    var getBasis = horizontal ? (
        style => ( style.flexBasis != "auto" ?
            parseFloat(style.flexBasis) :
            parseFloat(style.width) )
    ) : (
        style => ( style.flexBasis != "auto" ?
            parseFloat(style.flexBasis) :
            parseFloat(style.height) )
    )
    var separatorBasis = getBasis(window.getComputedStyle(separator));
    var list = dragging.before;
    var rect = container.getBoundingClientRect();
    if (horizontal) {
        dragging.min = rect.left;
        dragging.max = rect.right;
    } else {
        dragging.min = rect.top;
        dragging.max = rect.bottom;
    }
    dragging.min += separatorBasis / 2;
    dragging.max += separatorBasis / 2;
    var element;
    dragging.before = [];
    element = separator.previousElementSibling;
    while (element != null) {
        let style = window.getComputedStyle(element);
        if (element.classList.contains("pane")) {
            dragging.before.push({
                element: element,
                grow: parseFloat(style.flexGrow)
            });
        }
        dragging.min += getBasis(style);
        element = element.previousElementSibling;
    }
    dragging.after = [];
    element = separator.nextElementSibling;
    while (element != null) {
        let style = window.getComputedStyle(element);
        if (element.classList.contains("pane")) {
            dragging.after.push({
                element: element,
                grow: parseFloat(style.flexGrow)
            });
        }
        dragging.max -= getBasis(style);
        element = element.nextElementSibling;
    }
    if (
        dragging.before.length == 0 ||
        dragging.after.length == 0 ||
        dragging.max <= dragging.min
    ) {
        dragStop(null);
        return;
    }
    dragging.before.reverse();
    dragging.after.reverse();
    var panes = [].concat(dragging.before, dragging.after);
    var totalGrow = panes.reduce(
        ((total, {grow}) => total + grow),
        0 );
    if (totalGrow <= 0) {
        panes.forEach(item => { item.grow = 1 / paneCount; });
    } else {
        panes.forEach(item => { item.grow /= totalGrow; });
    }
    for (let {element, grow} of panes) {
        element.style.flexGrow = grow;
    }
}

function drag(event) {
    if (dragging == null) {
        dragStop(null);
    }
    var position = dragging.horizontal ?
        event.clientX : event.clientY;
    var portion = (position - dragging.min) / (dragging.max - dragging.min);
    if (isNaN(portion)) {
        return;
    }
    if (portion < 0) {
        portion = 0;
    } else if (portion > 1) {
        portion = 1;
    }
    var beforeDelta = portion, afterDelta = 1 - portion;
    for (let item of dragging.before) {
        beforeDelta -= item.grow;
        if (beforeDelta < 0) {
            item.grow += beforeDelta;
            beforeDelta = 0;
        }
    }
    if (beforeDelta > 0) {
        dragging.before[dragging.before.length - 1].grow += beforeDelta
    }
    for (let item of dragging.after) {
        afterDelta -= item.grow;
        if (afterDelta < 0) {
            item.grow += afterDelta;
            afterDelta = 0;
        }
    }
    if (afterDelta > 0) {
        dragging.after[dragging.after.length - 1].grow += afterDelta
    }
    for (let {element, grow} of dragging.before) {
        element.style.flexGrow = grow;
    }
    for (let {element, grow} of dragging.after) {
        element.style.flexGrow = grow;
    }
}

function dragStop(event) {
    console.log(event);
    window .removeEventListener("mousemove",  drag);
    window .removeEventListener("mouseup",    dragStop);
    overlay.removeEventListener("mouseleave", dragStop);
    overlay.style.zIndex = null;
    if (dragging == null)
        return;
    try {
        dragging.separator.classList.remove("pane--separator__active");
        if (event != null)
            drag(event);
        // XXX trigger paneresize on affected panes
    } finally {
        dragging = null;
    }
}

function insert(parent, index, element) {
    // parent must be .pane__horizontal or .pane__vertical
    // index must be either 0, -1, or a child pane of the parent
    let insert;
    if (index == -1 || parent.childElementCount == 0) {
        insert = (leaf) => { parent.appendChild(leaf); }
    } else if (index == 0) {
        let child = parent.firstElementChild;
        insert = (leaf) => { parent.insertBefore(leaf, child); }
    } else if (index instanceof Element) {
        insert = (leaf) => { parent.insertBefore(leaf, index); }
    } else {
        throw new Error( "invalid index " + index +
            " of type " + (typeof index) );
    }
    let fragment = document.createDocumentFragment();
    fragment.appendChild(makeLeaf(element));
    fragment.appendChild(makeSeparator());
    insert(fragment);
    return;
}

export default class PaneTree {

    constructor(root) {
        root.classList.add("pane", "pane__root");
        this.root = root;
    }

    insert(relative, direction, element) {
        // direction is 'above', 'below', 'leftward', 'rightward'.
        // relative is .pane element.
        // return new .pane__leaf containing the element

        // Suppose that direction is 'below'. Cases:
        // 1. relative is .pane__vertical
        //         =>  add .pane__leaf inside it
        // 2. relative is .pane__root
        //     a. root is empty
        //         =>  add .pane__leaf in it
        //     b. root contains .pane__leaf
        //         =>  create .pane__vertical in the root (containing the child),
        //             and add .pane__leaf inside it
        //     c. root contains .pane__horizontal
        //         =>  create .pane__vertical in the root (containing the child),
        //             and add .pane__leaf inside it
        //     d. root contains .pane__vertical
        //         =>  add .pane__leaf inside it
        // 3. relative is .pane__leaf
        //     a. parent is pane__root
        //         =>  create .pane__vertical in the root (containing the relative),
        //             and add .pane__leaf inside it
        //     b. parent is pane__horizontal
        //         =>  create .pane__vertical in the parent (containing the relative),
        //             and add .pane__leaf inside it
        //     c. parent is pane__vertical
        //         =>  add .pane__leaf inside it
        // 4. relative is .pane__horizontal
        //     a. parent is pane__root
        //         =>  create .pane__vertical in the root (containing the relative),
        //             and add .pane__leaf inside it
        //     b. parent is pane__vertical
        //         =>  add .pane__leaf inside it
        var parallel, perpendicular, before;
        if (direction == "above" || direction == "below") {
            parallel = "pane__vertical";
            perpendicular = "pane__horizontal";
        } else if (direction == "leftward" || direction == "rightward") {
            parallel = "pane__horizontal";
            perpendicular = "pane__vertical";
        } else {
            throw new Error( "invalid direction " + direction +
                " of type " + (typeof direction) );
        }
        if (direction == "above" || direction == "leftward") {
            before = true;
        } else if (direction == "below" || direction == "rightward") {
            before = false;
        }
        if (relative.classList.contains(parallel)) {
            insert(relative, before ? 0 : -1, element);
            return;
        }
        if (relative === this.root) {
            let child = this.root.firstElementChild;
            if (child == null) {
                this.root.appendChild(makeLeaf(element));
            } else if (child.classList.contains(parallel)) {
                insert(child, before ? 0 : -1, element);
            } else {
                let pane = document.createElement("div");
                pane.classList.add("pane", parallel);
                pane.appendChild(child);
                pane.appendChild(makeSeparator());
                insert(pane, before ? 0 : -1, element);
                this.root.appendChild(pane);
            }
            return;
        }
        var parent = relative.parentElement;
        if (parent.classList.contains(parallel)) {
            if (before) {
                insert(parent, relative, element);
            } else {
                let index = relative.nextElementSibling;
                while (index != null && !index.classList.contains("pane")) {
                    index = index.netElementSibling;
                }
                insert(parent, index != null ? index : -1, element);
            }
            return;
        }
        if (parent === this.root) {
            let pane = document.createElement("div");
            pane.classList.add("pane", parallel);
            pane.appendChild(relative);
            pane.appendChild(makeSeparator());
            insert(pane, before ? 0 : -1, element);
            this.root.appendChild(pane);
            return;
        }
        if (parent.classList.contains(perpendicular)) {
            let pane = document.createElement("div");
            pane.classList.add("pane", parallel);
            parent.insertBefore(pane, relative);
            pane.appendChild(relative);
            pane.appendChild(makeSeparator());
            insert(pane, before ? 0 : -1, element);
            return;
        }
        throw new Error("invalid pane tree structure");
    }

    remove(element) {
        // element must be .pane__leaf
        // or not? we have to be able to cleanup the panes…
        throw new Error("XXX not implemented");
    }

    transpose() {
        throw new Error("XXX not implemented");
    }

}

