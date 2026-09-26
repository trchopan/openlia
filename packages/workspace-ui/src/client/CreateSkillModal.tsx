import { useEffect, useId, useState } from "react";

export interface CreateSkillModalProps {
  isOpen: boolean;
  onClose: () => void;
  categories: string[];
  onCreate: (
    name: string,
    category: string,
    description: string,
  ) => Promise<void>;
  isCreating?: boolean;
}

export function CreateSkillModal({
  isOpen,
  onClose,
  categories,
  onCreate,
  isCreating = false,
}: CreateSkillModalProps) {
  const nameId = useId();
  const categoryId = useId();
  const descriptionId = useId();

  const [name, setName] = useState("");
  const [category, setCategory] = useState("");
  const [customCategory, setCustomCategory] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (isOpen) {
      setName("");
      setCategory("");
      setCustomCategory("");
      setDescription("");
      setError("");
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanName = name.trim().toLowerCase();
    if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(cleanName)) {
      setError(
        "Skill name must be lowercase kebab-case (e.g. 'my-awesome-skill').",
      );
      return;
    }
    const finalCategory =
      category === "__new__" ? customCategory.trim().toLowerCase() : category;

    if (finalCategory && !/^[a-z0-9_-]+$/.test(finalCategory)) {
      setError(
        "Category must contain only letters, numbers, dashes, and underscores.",
      );
      return;
    }

    try {
      setError("");
      await onCreate(cleanName, finalCategory, description.trim());
      onClose();
    } catch (caught: unknown) {
      if (caught instanceof Error) {
        setError(caught.message);
      } else {
        setError("Failed to create skill.");
      }
    }
  };

  return (
    <div className="modal modal-open">
      <div className="modal-box max-w-md">
        <h3 className="text-lg font-bold">Create New Skill</h3>
        <p className="mt-1 text-xs text-base-content/60">
          Scaffold a new skill directory with a starter SKILL.md instruction.
        </p>

        {error && (
          <div className="alert alert-error mt-4 text-xs">
            <span>{error}</span>
          </div>
        )}

        <form className="mt-4 space-y-4" onSubmit={handleSubmit}>
          <div>
            <label className="text-xs font-semibold" htmlFor={nameId}>
              Skill Name (kebab-case) *
            </label>
            <input
              className="input input-bordered input-sm mt-1 w-full"
              disabled={isCreating}
              id={nameId}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. invoice-parser"
              required
              type="text"
              value={name}
            />
          </div>

          <div>
            <label className="text-xs font-semibold" htmlFor={categoryId}>
              Category
            </label>
            <select
              className="select select-bordered select-sm mt-1 w-full"
              disabled={isCreating}
              id={categoryId}
              onChange={(e) => setCategory(e.target.value)}
              value={category}
            >
              <option value="">Standalone (Root)</option>
              {categories.map((cat) => (
                <option key={cat} value={cat}>
                  {cat}
                </option>
              ))}
              <option value="__new__">+ New Category...</option>
            </select>
          </div>

          {category === "__new__" && (
            <div>
              <label className="text-xs font-semibold" htmlFor="new-cat">
                New Category Name
              </label>
              <input
                className="input input-bordered input-sm mt-1 w-full"
                disabled={isCreating}
                id="new-cat"
                onChange={(e) => setCustomCategory(e.target.value)}
                placeholder="e.g. finance"
                required
                type="text"
                value={customCategory}
              />
            </div>
          )}

          <div>
            <label className="text-xs font-semibold" htmlFor={descriptionId}>
              Description
            </label>
            <textarea
              className="textarea textarea-bordered textarea-sm mt-1 w-full"
              disabled={isCreating}
              id={descriptionId}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Summarize what this skill does and when Hermes should invoke it..."
              rows={3}
              value={description}
            />
          </div>

          <div className="modal-action mt-6">
            <button
              className="btn btn-ghost btn-sm"
              disabled={isCreating}
              onClick={onClose}
              type="button"
            >
              Cancel
            </button>
            <button
              className="btn btn-primary btn-sm"
              disabled={isCreating || !name.trim()}
              type="submit"
            >
              {isCreating ? "Creating..." : "Create Skill"}
            </button>
          </div>
        </form>
      </div>
      <button
        className="modal-backdrop bg-black/40"
        onClick={onClose}
        type="button"
      >
        close
      </button>
    </div>
  );
}
