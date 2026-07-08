(function () {
  var nav = document.getElementById('nav');
  window.addEventListener('scroll', function () {
    nav.classList.toggle('scrolled', window.scrollY > 12);
  });

  var reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  var revealTargets = document.querySelectorAll('.reveal');
  if (reduceMotion) {
    revealTargets.forEach(function (el) { el.classList.add('in-view'); });
  } else {
    var io = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            entry.target.classList.add('in-view');
            io.unobserve(entry.target);
          }
        });
      },
      { threshold: 0.15 },
    );
    revealTargets.forEach(function (el, i) {
      el.style.animationDelay = Math.min(i % 6, 5) * 0.06 + 's';
      io.observe(el);
    });
  }

  var counters = document.querySelectorAll('.stat-num');
  var countersDone = false;
  function animateCounters() {
    if (countersDone) return;
    countersDone = true;
    counters.forEach(function (el) {
      var target = parseFloat(el.getAttribute('data-count'));
      var suffix = el.getAttribute('data-suffix') || '';
      if (reduceMotion) {
        el.textContent = target + suffix;
        return;
      }
      var start = performance.now();
      var duration = 1100;
      function tick(now) {
        var p = Math.min(1, (now - start) / duration);
        var eased = 1 - Math.pow(1 - p, 3);
        el.textContent = Math.round(target * eased) + suffix;
        if (p < 1) requestAnimationFrame(tick);
      }
      requestAnimationFrame(tick);
    });
  }
  var statsSection = document.querySelector('.stats');
  if (statsSection) {
    var statsIo = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            animateCounters();
            statsIo.disconnect();
          }
        });
      },
      { threshold: 0.4 },
    );
    statsIo.observe(statsSection);
  }

  var termEl = document.getElementById('termType');
  var termLine = 'bash <(curl -sSL raw.githubusercontent.com/superbodik/PowerNode/main/install.sh)';
  if (termEl) {
    if (reduceMotion) {
      termEl.textContent = termLine;
    } else {
      var i = 0;
      function typeNext() {
        if (i <= termLine.length) {
          termEl.textContent = termLine.slice(0, i);
          i += 1;
          setTimeout(typeNext, 22);
        }
      }
      setTimeout(typeNext, 500);
    }
  }

  function wireCopy(buttonId, sourceId) {
    var btn = document.getElementById(buttonId);
    var src = document.getElementById(sourceId);
    if (!btn || !src) return;
    btn.addEventListener('click', function () {
      var text = src.textContent.trim();
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(function () {
          btn.textContent = 'Copied!';
          btn.classList.add('copied');
          setTimeout(function () {
            btn.textContent = 'Copy';
            btn.classList.remove('copied');
          }, 1600);
        });
      }
    });
  }
  wireCopy('copyInstall', 'installCmd');
  wireCopy('copyUpdate', 'updateCmd');
})();
