(function () {
  'use strict';

  var THEME_KEY = 'romharbor-theme';
  var JOBS_INTERVAL = 5000;
  var JOB_INTERVAL = 2000;

  function storedTheme() {
    try {
      return window.localStorage.getItem(THEME_KEY);
    } catch (error) {
      return null;
    }
  }

  function storeTheme(theme) {
    try {
      window.localStorage.setItem(THEME_KEY, theme);
    } catch (error) {
      return;
    }
  }

  function systemTheme() {
    if (!window.matchMedia) {
      return 'light';
    }
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    var toggle = document.getElementById('theme-toggle');
    if (!toggle) {
      return;
    }
    var dark = theme === 'dark';
    toggle.setAttribute('aria-pressed', dark ? 'true' : 'false');
    toggle.setAttribute('aria-label', dark ? 'Switch to light theme' : 'Switch to dark theme');
  }

  function wireTheme() {
    var toggle = document.getElementById('theme-toggle');
    var current = document.documentElement.getAttribute('data-theme') || systemTheme();
    applyTheme(current);
    if (!toggle) {
      return;
    }
    toggle.addEventListener('click', function () {
      var next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      applyTheme(next);
      storeTheme(next);
    });
    if (!window.matchMedia) {
      return;
    }
    var query = window.matchMedia('(prefers-color-scheme: dark)');
    var follow = function () {
      if (storedTheme() === null) {
        applyTheme(systemTheme());
      }
    };
    if (query.addEventListener) {
      query.addEventListener('change', follow);
    } else if (query.addListener) {
      query.addListener(follow);
    }
  }

  function element(tag, className, text) {
    var node = document.createElement(tag);
    if (className) {
      node.className = className;
    }
    if (text !== undefined && text !== null) {
      node.textContent = text;
    }
    return node;
  }

  function humanSize(bytes) {
    var units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];
    var value = Number(bytes) || 0;
    var unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value = value / 1024;
      unit = unit + 1;
    }
    if (unit === 0) {
      return value + ' ' + units[0];
    }
    return value.toFixed(1) + ' ' + units[unit];
  }

  function jobPercent(job) {
    if (job.status === 'done') {
      return 100;
    }
    if (!job.total) {
      return 0;
    }
    var value = Math.round((job.downloaded / job.total) * 100);
    if (value < 0) {
      return 0;
    }
    if (value > 100) {
      return 100;
    }
    return value;
  }

  function jobPlatform(job) {
    if (!job.path) {
      return '';
    }
    var slash = job.path.indexOf('/');
    if (slash <= 0) {
      return '';
    }
    return job.path.slice(0, slash);
  }

  function jobElement(job) {
    var percent = jobPercent(job);
    var article = element('article', 'job');
    article.setAttribute('data-job-id', job.id);
    article.setAttribute('data-status', job.status);

    var head = element('div', 'job-head');
    head.appendChild(element('h3', 'job-title', job.title || job.id));
    head.appendChild(element('span', 'chip chip-' + job.status, job.status));
    article.appendChild(head);

    var platform = jobPlatform(job);
    if (platform) {
      article.appendChild(element('p', 'job-meta', platform));
    }

    var progress = element('div', 'progress');
    progress.setAttribute('role', 'progressbar');
    progress.setAttribute('aria-valuemin', '0');
    progress.setAttribute('aria-valuemax', '100');
    progress.setAttribute('aria-valuenow', String(percent));
    progress.setAttribute('aria-label', 'Progress of ' + (job.title || job.id));
    var bar = element('span', 'progress-bar');
    bar.style.width = percent + '%';
    progress.appendChild(bar);
    article.appendChild(progress);

    if (job.error) {
      article.appendChild(element('p', 'job-note', job.error));
    } else if (job.path) {
      var note = element('p', 'job-note');
      note.appendChild(document.createTextNode('Saved to '));
      note.appendChild(element('code', null, job.path));
      article.appendChild(note);
    } else if (job.total) {
      article.appendChild(element('p', 'job-note', humanSize(job.downloaded) + ' of ' + humanSize(job.total)));
    } else {
      article.appendChild(element('p', 'job-note', 'Waiting to start'));
    }
    return article;
  }

  function renderJobs(jobs) {
    var list = document.getElementById('downloads-list');
    if (!list) {
      return;
    }
    list.textContent = '';
    if (!jobs.length) {
      list.appendChild(element('p', 'empty', 'No downloads yet.'));
    } else {
      jobs.forEach(function (job) {
        list.appendChild(jobElement(job));
      });
    }
    var active = jobs.filter(function (job) {
      return job.status === 'queued' || job.status === 'running';
    }).length;
    var count = document.getElementById('downloads-count');
    if (count) {
      count.textContent = String(active);
      count.setAttribute('data-active', active > 0 ? 'true' : 'false');
    }
  }

  var jobsETag = '';

  function refreshJobs() {
    if (document.hidden) {
      return;
    }
    var panel = document.getElementById('downloads-panel');
    if (!panel) {
      return;
    }
    var headers = {};
    if (jobsETag) {
      headers['If-None-Match'] = jobsETag;
    }
    var jobsEndpoint = panel.getAttribute('data-jobs-endpoint') || '/api/jobs';
    fetch(jobsEndpoint, { headers: headers, cache: 'no-store' })
      .then(function (response) {
        if (response.status === 304) {
          return null;
        }
        if (!response.ok) {
          throw new Error('the jobs request failed');
        }
        var etag = response.headers.get('ETag');
        if (etag) {
          jobsETag = etag;
        }
        return response.json();
      })
      .then(function (payload) {
        if (!payload) {
          return;
        }
        renderJobs(payload.jobs || []);
      })
      .catch(function () {
        return;
      });
  }

  function wireDownloads() {
    var panel = document.getElementById('downloads-panel');
    var toggle = document.getElementById('downloads-toggle');
    if (!panel || !toggle) {
      return;
    }
    var open = function (state) {
      panel.setAttribute('data-open', state ? 'true' : 'false');
      toggle.setAttribute('aria-expanded', state ? 'true' : 'false');
      if (state) {
        refreshJobs();
      }
    };
    toggle.addEventListener('click', function () {
      open(panel.getAttribute('data-open') !== 'true');
    });
    var close = document.querySelector('[data-close-downloads]');
    if (close) {
      close.addEventListener('click', function () {
        open(false);
        toggle.focus();
      });
    }
    document.addEventListener('keydown', function (event) {
      if (event.key === 'Escape' && panel.getAttribute('data-open') === 'true') {
        open(false);
        toggle.focus();
      }
    });
    document.addEventListener('visibilitychange', function () {
      if (!document.hidden) {
        refreshJobs();
      }
    });
    window.setInterval(refreshJobs, JOBS_INTERVAL);
  }

  function watch(button, job) {
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    button.setAttribute('data-state', 'running');
    var timer = window.setInterval(function () {
      fetch('/api/job?id=' + encodeURIComponent(job.id), { cache: 'no-store' })
        .then(function (response) {
          return response.json();
        })
        .then(function (state) {
          if (state.status === 'done' || state.status === 'failed') {
            window.clearInterval(timer);
            button.disabled = false;
            button.removeAttribute('aria-busy');
            button.setAttribute('data-state', state.status);
          }
          refreshJobs();
        })
        .catch(function () {
          return;
        });
    }, JOB_INTERVAL);
  }

  function wireDownloadsButtons() {
    var buttons = document.querySelectorAll('button.dl');
    Array.prototype.forEach.call(buttons, function (button) {
      button.addEventListener('click', function () {
        button.disabled = true;
        button.setAttribute('aria-busy', 'true');
        fetch('/api/download', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id: button.dataset.id })
        })
          .then(function (response) {
            return response.json();
          })
          .then(function (job) {
            if (job && job.id) {
              watch(button, job);
            } else {
              button.disabled = false;
              button.removeAttribute('aria-busy');
            }
            refreshJobs();
          })
          .catch(function () {
            button.disabled = false;
            button.removeAttribute('aria-busy');
          });
      });
    });
  }

  function wireCovers() {
    Array.prototype.forEach.call(document.querySelectorAll('img.cover'), function (image) {
      function loaded() {
        image.classList.add('is-loaded');
      }
      function failed() {
        image.classList.add('is-failed');
      }
      image.addEventListener('load', loaded);
      image.addEventListener('error', failed);
      if (image.complete) {
        if (image.naturalWidth > 0) {
          loaded();
        } else {
          failed();
        }
      }
    });
  }

  wireTheme();
  wireCovers();
  wireDownloads();
  wireDownloadsButtons();
})();
